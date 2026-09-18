package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cockroachdb/pebble"
)

// Command pebble-rebuild rebuilds a fragmented PebbleDB store into a fresh one
// with far fewer, larger sstables by streaming every key/value pair into a new
// store opened with growing per-level target file sizes. This heals an
// already-degraded store, which an in-place db.Compact cannot: non-overlapping
// bottom-level (L6) files are never rewritten for size.
//
// The source is opened read-only and never modified, so an aborted run is always
// safe. A full copy (the default) is verified against the source (key count +
// byte total) before "VERIFY OK"; only then is it safe to delete the source. The
// destination is then chown'd to match the source's owner, so a run under sudo
// still produces a store the node's (non-root) user can open. --sample-gb
// produces an incomplete store for measurement only.
//
//	pebble-rebuild [flags] <src-db> <dst-db>
func main() {
	var (
		sampleGB    = flag.Float64("sample-gb", 0, "stop after copying this many GiB (0 = full copy). MEASUREMENT ONLY — produces an INCOMPLETE store; disables --verify.")
		progressSec = flag.Int("progress-sec", 15, "progress report interval in seconds")
		batchMB     = flag.Int("batch-mb", 64, "flush the destination batch when it reaches this many MiB")
		verify      = flag.Bool("verify", true, "after a full copy, re-scan the destination and assert key count + byte total match the source (auto-disabled for --sample-gb runs)")
		force       = flag.Bool("force", false, "allow writing into a non-empty destination directory (default: refuse, to avoid appending onto a half-built store)")
		matchOwner  = flag.Bool("match-owner", true, "chown the destination to the source's uid/gid after a full copy (so a sudo run still yields a store the node's user can open)")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: pebble-rebuild [flags] <src-db> <dst-db>\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	src, dst := flag.Arg(0), flag.Arg(1)
	full := *sampleGB == 0
	if !full {
		*verify = false // an incomplete store can never match the source
	}
	maxBytes := int64(*sampleGB * (1 << 30))

	// Refuse to write into a non-empty destination unless forced: a leftover
	// half-built store from an aborted run would be silently appended to.
	if full {
		if ents, err := os.ReadDir(dst); err == nil && len(ents) > 0 && !*force {
			fmt.Printf("FATAL: destination %q is not empty; remove it or pass --force\n", dst)
			os.Exit(1)
		}
	}

	res, err := rebuild(src, dst, maxBytes, *batchMB<<20, *progressSec, true)
	if err != nil {
		fmt.Printf("FATAL: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("COPIED %.1fGiB keys=%d in %s | dst_files=%d dst_size=%.1fGiB (%.1f files/GiB) rss=%dMB\n",
		float64(res.bytes)/(1<<30), res.keys, res.elapsed.Round(time.Second),
		res.dstFiles, res.dstSize, ratio(float64(res.dstFiles), res.dstSize), rssMB())

	if full {
		if *verify {
			fmt.Println("verifying (re-scanning the destination — this is silent and can take a while on a large store)...")
			vKeys, vBytes, err := scanStore(dst, true, *progressSec)
			if err != nil {
				fmt.Printf("FATAL: verify: %v\n", err)
				os.Exit(1)
			}
			if vKeys != res.keys || vBytes != res.bytes {
				fmt.Printf("\nVERIFY FAILED: dst keys=%d bytes=%d != src keys=%d bytes=%d — DO NOT delete the source\n",
					vKeys, vBytes, res.keys, res.bytes)
				os.Exit(1)
			}
			fmt.Printf("\nVERIFY OK: dst keys=%d bytes=%d == src (safe to swap in; delete source only after the node reaches head)\n",
				vKeys, vBytes)
		}
		if res.srcFiles > 0 && res.dstFiles > 0 {
			fmt.Printf("files: source=%d rebuilt=%d reduction=%.0fx\n",
				res.srcFiles, res.dstFiles, ratio(float64(res.srcFiles), float64(res.dstFiles)))
		}
		if *matchOwner {
			if uid, gid, err := chownToMatch(src, dst); err != nil {
				fmt.Printf("WARNING: could not set owner on %s to match %s: %v\n"+
					"  the node may fail to open it — fix manually: sudo chown -R <node-user>:<group> %s\n", dst, src, err, dst)
			} else {
				fmt.Printf("owner: %s set to uid=%d gid=%d (matches source)\n", dst, uid, gid)
			}
		}
		return
	}

	// Sample run: extrapolate the full-store file count and duration from the
	// measured slice (files-per-GiB scales; the tiny-vs-large file regime is set
	// by the target file sizes, not by how much was copied).
	dstFPG := ratio(float64(res.dstFiles), res.dstSize)
	throughput := ratio(float64(res.bytes)/(1<<30), res.elapsed.Seconds())
	fmt.Printf("\n=== EXTRAPOLATION to full source (%.0fGiB) ===\n", res.srcSize)
	fmt.Printf("files: source=%d rebuilt~=%.0f reduction=%.0fx\n",
		res.srcFiles, res.srcSize*dstFPG, ratio(ratio(float64(res.srcFiles), res.srcSize), dstFPG))
	fmt.Printf("throughput=%.2fGiB/s -> full rebuild ~= %.1fh\n", throughput, ratio(res.srcSize, throughput)/3600)
}

type result struct {
	keys, bytes          int64
	srcFiles, dstFiles   int64
	srcSize, dstSize     float64
	srcFormat, dstFormat string
	elapsed              time.Duration
}

// rebuild streams every KV pair from src (read-only) into a fresh dst. It never
// holds the whole store in memory: writes are batched to batchFlushAt bytes and
// flushed, so RSS is bounded regardless of store size. maxBytes>0 stops early
// (incomplete — measurement only).
func rebuild(src, dst string, maxBytes int64, batchFlushAt, progressSec int, progress bool) (result, error) {
	var r result

	sdb, err := pebble.Open(src, &pebble.Options{ReadOnly: true})
	if err != nil {
		return r, fmt.Errorf("open src: %w", err)
	}
	defer sdb.Close()
	r.srcFiles, r.srcSize = files(sdb)
	r.srcFormat = sdb.FormatMajorVersion().String()
	if progress {
		fmt.Printf("SOURCE: %s files=%d size=%.1fGiB (%.0f files/GiB) format=%s\n",
			src, r.srcFiles, r.srcSize, ratio(float64(r.srcFiles), r.srcSize), r.srcFormat)
	}

	ddb, err := pebble.Open(dst, buildTunedOptions())
	if err != nil {
		return r, fmt.Errorf("open dst: %w", err)
	}
	r.dstFormat = ddb.FormatMajorVersion().String()
	if progress {
		fmt.Printf("DEST:   %s format=%s (opens at FormatMostCompatible => readable by older / default-pebble binaries)\n",
			dst, r.dstFormat)
	}

	iter, err := sdb.NewIter(nil)
	if err != nil {
		ddb.Close()
		return r, fmt.Errorf("new iter: %w", err)
	}
	batch := ddb.NewBatch()
	lastReport := time.Now()
	t0 := time.Now()
	flush := func() error {
		if batch.Count() > 0 {
			if err := ddb.Apply(batch, pebble.NoSync); err != nil {
				return err
			}
			batch.Reset()
		}
		return nil
	}
	for iter.First(); iter.Valid(); iter.Next() {
		k, v := iter.Key(), iter.Value()
		_ = batch.Set(k, v, nil) // Batch.Set copies k/v internally — safe across Next()
		r.bytes += int64(len(k) + len(v))
		r.keys++
		if batch.Len() >= batchFlushAt {
			if err := flush(); err != nil {
				iter.Close()
				ddb.Close()
				return r, fmt.Errorf("apply: %w", err)
			}
		}
		if progress && time.Since(lastReport) >= time.Duration(progressSec)*time.Second {
			df, dsz := files(ddb)
			rate := float64(r.bytes) / (1 << 30) / time.Since(t0).Seconds()
			fmt.Printf("t=%4.0fs copied=%.1fGiB keys=%d rss=%dMB dst_files=%d dst=%.1fGiB rate=%.2fGiB/s\n",
				time.Since(t0).Seconds(), float64(r.bytes)/(1<<30), r.keys, rssMB(), df, dsz, rate)
			lastReport = time.Now()
		}
		if maxBytes > 0 && r.bytes >= maxBytes {
			if progress {
				fmt.Println("reached --sample-gb, stopping copy (INCOMPLETE store)")
			}
			break
		}
	}
	if err := iter.Error(); err != nil {
		iter.Close()
		ddb.Close()
		return r, fmt.Errorf("iterate: %w", err)
	}
	if err := flush(); err != nil {
		iter.Close()
		ddb.Close()
		return r, fmt.Errorf("final apply: %w", err)
	}
	iter.Close()
	if progress {
		fmt.Println("flushing dst (may trigger final compactions)...")
	}
	if err := ddb.Flush(); err != nil {
		ddb.Close()
		return r, fmt.Errorf("flush: %w", err)
	}
	r.dstFiles, r.dstSize = files(ddb)
	r.elapsed = time.Since(t0)
	if err := ddb.Close(); err != nil {
		return r, fmt.Errorf("close dst: %w", err)
	}
	return r, nil
}

// scanStore reopens a store read-only and returns its total key count and summed
// key+value bytes, for verifying a rebuilt store against its source. It prints a
// periodic progress line so a long scan (billions of keys) does not look hung.
func scanStore(dir string, progress bool, progressSec int) (keys, bytes int64, err error) {
	db, err := pebble.Open(dir, &pebble.Options{ReadOnly: true})
	if err != nil {
		return 0, 0, fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	it, err := db.NewIter(nil)
	if err != nil {
		return 0, 0, fmt.Errorf("iter: %w", err)
	}
	defer it.Close()
	last := time.Now()
	for it.First(); it.Valid(); it.Next() {
		keys++
		bytes += int64(len(it.Key()) + len(it.Value()))
		if progress && time.Since(last) >= time.Duration(progressSec)*time.Second {
			fmt.Printf("verify: scanned %d keys...\n", keys)
			last = time.Now()
		}
	}
	if err := it.Error(); err != nil {
		return 0, 0, fmt.Errorf("iterate: %w", err)
	}
	return keys, bytes, nil
}

// chownToMatch recursively sets the owner of dst to the uid/gid that owns src, so
// a rebuild run under sudo (root) still yields a store the node's own (non-root)
// user can open — otherwise the node crash-loops on "permission denied" opening
// the store's LOCK. It is a no-op-equivalent when already correctly owned, and
// returns an error (not fatal) when the caller lacks permission to chown.
func chownToMatch(src, dst string) (uid, gid int, err error) {
	fi, err := os.Stat(src)
	if err != nil {
		return 0, 0, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("cannot read owner of %s (unsupported platform)", src)
	}
	uid, gid = int(st.Uid), int(st.Gid)
	err = filepath.Walk(dst, func(p string, _ os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Lchown(p, uid, gid)
	})
	return uid, gid, err
}

func files(db *pebble.DB) (int64, float64) {
	t := db.Metrics().Total()
	return t.NumFiles, float64(t.Size) / (1 << 30)
}

func ratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func rssMB() int {
	b, _ := os.ReadFile("/proc/self/status")
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "VmRSS:") {
			f := strings.Fields(ln)
			v, _ := strconv.Atoi(f[1])
			return v / 1024
		}
	}
	return 0
}

// buildTunedOptions opens the destination with growing per-level target file
// sizes (8→128 MiB) so rewritten data lands in large L6 files instead of the
// source's tiny ones. FormatMajorVersion is left unset (FormatMostCompatible) so
// the rebuilt store stays openable by older / default-pebble binaries. The
// values mirror celestia-core's db_tuning flag, so enabling that flag keeps the
// rebuilt store healthy going forward.
func buildTunedOptions() *pebble.Options {
	const (
		memTableSize uint64 = 64 << 20
		l0Target     int64  = 8 << 20
		maxTarget    int64  = 128 << 20
		numLevels           = 7
	)
	levels := make([]pebble.LevelOptions, numLevels)
	target := l0Target
	for i := range levels {
		levels[i].TargetFileSize = target
		if target < maxTarget {
			target *= 2
		}
	}
	return &pebble.Options{
		MemTableSize:                memTableSize,
		MemTableStopWritesThreshold: 4,
		L0StopWritesThreshold:       24,
		MaxConcurrentCompactions:    func() int { return max(2, runtime.GOMAXPROCS(0)/4) },
		Levels:                      levels,
		Cache:                       pebble.NewCache(512 << 20),
	}
}
