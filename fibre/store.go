package fibre

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"
	gogoproto "github.com/cosmos/gogoproto/proto"
)

// Layout under [StoreConfig.Path]:
//
//	shards/<commit>-<hash>  finalized shard payloads (flat files).
//	staging/<rand>          in-flight Put writes; renamed into shards/ on
//	                        success, dropped wholesale by [Store.reconcile]
//	                        on next open.
//
// Bulk shard data is kept off pebble because pebble serializes large-value
// commits through a single goroutine, which becomes the upload bottleneck at
// concurrency. Pebble only holds the small metadata.
const (
	shardsSubdir  = "shards"
	stagingSubdir = "staging"
)

// ErrStoreNotFound is returned when no shard is found for a [Commitment] in the [Store].
var ErrStoreNotFound = errors.New("no shard found in store")

// StoreConfig contains configuration options for the [Store].
type StoreConfig struct {
	// Path is the path to the store directory.
	Path string `toml:"-"`
	// Log defaults to [slog.Default] when nil.
	Log *slog.Logger `toml:"-"`
}

// DefaultStoreConfig returns a [StoreConfig] with default values.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{}
}

// Validate checks that the StoreConfig is valid.
func (cfg StoreConfig) Validate() error {
	if cfg.Path == "" {
		return fmt.Errorf("store path is required")
	}
	return nil
}

// Store manages persistent storage of [PaymentPromise] and row data.
// It provides indexed access by [Commitment], promise hash, and timestamp.
type Store struct {
	cfg StoreConfig
	db  *pebbledb.DB
	fs  vfs.FS
	log *slog.Logger
}

// shardWriteCategory identifies our shard-file writes in pebble's vfs disk
// I/O telemetry.
const shardWriteCategory vfs.DiskWriteCategory = "fibre-shard"

// memStorePath is an arbitrary location inside the in-memory FS used by
// [NewMemoryStore]; both pebble's files and our shards/staging subdirs live
// under it so the layout matches the on-disk store.
const memStorePath = "/store"

// NewMemoryStore creates a [Store] backed entirely by [vfs.NewMem]; both the
// pebble metadata and the flat shard files live in memory and are dropped
// when the Store is garbage collected.
func NewMemoryStore(cfg StoreConfig) *Store {
	cfg.Path = memStorePath
	s, err := openStore(cfg, vfs.NewMem())
	if err != nil {
		panic(fmt.Sprintf("opening in-memory store: %v", err))
	}
	return s
}

// NewStore opens a [Store] backed by an on-disk pebble database and flat
// shard files at cfg.Path. On open, [Store.reconcile] drops any leftover
// staging files from a previous crash.
func NewStore(cfg StoreConfig) (*Store, error) {
	return openStore(cfg, vfs.Default)
}

func openStore(cfg StoreConfig, filesystem vfs.FS) (*Store, error) {
	for _, sub := range []string{shardsSubdir, stagingSubdir} {
		if err := filesystem.MkdirAll(filepath.Join(cfg.Path, sub), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s directory: %w", sub, err)
		}
	}

	opts := &pebbledb.Options{FS: filesystem}
	// Values in pebble are sub-1KB metadata only; tuning is light.
	opts.MemTableSize = 16 << 20
	opts.L0CompactionThreshold = 4
	opts.L0StopWritesThreshold = 12
	opts.LBaseMaxBytes = 64 << 20

	db, err := pebbledb.Open(cfg.Path, opts)
	if err != nil {
		return nil, fmt.Errorf("opening pebble database: %w", err)
	}

	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	s := &Store{cfg: cfg, db: db, fs: filesystem, log: log}
	if err := s.reconcile(); err != nil {
		_ = s.db.Close()
		return nil, fmt.Errorf("reconciling store: %w", err)
	}
	return s, nil
}

// Put stores a [PaymentPromise] and [types.BlobShard] using a stage → commit
// → publish pattern: write tmp under staging/, commit pebble metadata, then
// rename into shards/<commit>-<hash>. A crash between commit and rename
// leaves a phantom marker that [Store.Get] cleans lazily and [PruneBefore]
// sweeps at pruneAt.
//
// Puts for the same commitment but different promises are stored independently
// without deduplication.
func (s *Store) Put(_ context.Context, promise *PaymentPromise, shard *types.BlobShard, pruneAt time.Time) error {
	promiseHash, err := promise.Hash()
	if err != nil {
		return fmt.Errorf("getting promise hash: %w", err)
	}

	tmp, err := s.writeTmpShard(shard)
	if err != nil {
		return fmt.Errorf("writing shard tmp: %w", err)
	}
	finalPath := s.shardFilePath(promise.Commitment, promiseHash)

	promiseProto, err := promise.ToProto()
	if err != nil {
		_ = s.fs.Remove(tmp)
		return fmt.Errorf("converting payment promise to proto: %w", err)
	}
	ppData, err := gogoproto.Marshal(promiseProto)
	if err != nil {
		_ = s.fs.Remove(tmp)
		return fmt.Errorf("marshaling payment promise: %w", err)
	}
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := batch.Set(promiseKey(promiseHash), ppData, pebbledb.NoSync); err != nil {
		_ = s.fs.Remove(tmp)
		return fmt.Errorf("putting payment promise: %w", err)
	}
	// Empty value: the marker only exists so [Get] can iterate by commitment.
	if err := batch.Set(shardKey(promise.Commitment, promiseHash), nil, pebbledb.NoSync); err != nil {
		_ = s.fs.Remove(tmp)
		return fmt.Errorf("putting shard marker: %w", err)
	}
	if err := batch.Set(pruneKey(pruneAt, promise.Commitment, promiseHash), nil, pebbledb.NoSync); err != nil {
		_ = s.fs.Remove(tmp)
		return fmt.Errorf("putting prune index: %w", err)
	}
	if err := batch.Commit(pebbledb.NoSync); err != nil {
		_ = s.fs.Remove(tmp)
		return fmt.Errorf("committing metadata: %w", err)
	}

	if err := s.fs.Rename(tmp, finalPath); err != nil {
		_ = s.fs.Remove(tmp)
		return fmt.Errorf("renaming shard tmp to final: %w", err)
	}
	return nil
}

// writeTmpShard stages a shard under <store>/staging/ at a randomly named
// file. Random (not canonical staging/<commit>-<hash>) because vfs.FS.Create
// truncates on collision rather than failing — no O_EXCL — so two concurrent
// same-key writers would clobber each other's tmp. Random per-writer names
// sidestep that; the rename in [Store.Put] picks one winner.
func (s *Store) writeTmpShard(shard *types.BlobShard) (string, error) {
	var rnd [16]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", fmt.Errorf("generating tmp name: %w", err)
	}
	tmp := filepath.Join(s.cfg.Path, stagingSubdir, hex.EncodeToString(rnd[:]))

	f, err := s.fs.Create(tmp, shardWriteCategory)
	if err != nil {
		return "", fmt.Errorf("creating tmp shard file: %w", err)
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	if err := writeShardBinary(bw, shard); err != nil {
		f.Close()
		_ = s.fs.Remove(tmp)
		return "", err
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		_ = s.fs.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = s.fs.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// shardFilePath returns the canonical flat-file path for (commit, hash). All
// shard files live as siblings under <store>/shards/; the (commit, hash) pair
// is encoded in the filename as <commit-hex>-<hash-hex>.
func (s *Store) shardFilePath(commit Commitment, promiseHash []byte) string {
	return filepath.Join(s.cfg.Path, shardsSubdir, commit.String()+"-"+hex.EncodeToString(promiseHash))
}

// Get returns the first [types.BlobShard] found for the given [Commitment].
// When multiple promises exist for the same commitment, returning only the
// first prevents unbounded message sizes; pebble's deterministic key order
// makes the choice consistent across validators.
//
// Get may write to pebble: if a /shard/ marker is found but the backing file
// is missing (crash leftover or pebble.NoSync power loss), the marker is
// deleted inline so future Gets stop paying the missed lookup.
func (s *Store) Get(_ context.Context, commitment Commitment) (*types.BlobShard, error) {
	prefix := fmt.Appendf(nil, "/shard/%s/", commitment.String())
	iter, err := s.db.NewIter(&pebbledb.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("creating iterator: %w", err)
	}
	defer iter.Close()

	var rerr error
	for valid := iter.First(); valid; valid = iter.Next() {
		promiseHashHex := string(iter.Key()[len(prefix):])
		promiseHash, err := hex.DecodeString(promiseHashHex)
		if err != nil {
			rerr = errors.Join(rerr, fmt.Errorf("decoding promise hash from shard key: %w", err))
			continue
		}

		shard, err := readShardFile(s.fs, s.shardFilePath(commitment, promiseHash))
		if err == nil {
			return shard, nil
		}
		if errors.Is(err, ErrStoreNotFound) {
			// Orphan marker — drop it. The /prune/ entry self-cleans at TTL.
			if delErr := s.db.Delete(shardKey(commitment, promiseHash), pebbledb.NoSync); delErr != nil {
				s.log.Warn("failed to clean orphan shard marker",
					"commitment", commitment.String(),
					"error", delErr,
				)
			}
			continue
		}
		rerr = errors.Join(rerr, fmt.Errorf("reading shard file: %w", err))
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterating shards: %w", err)
	}
	if rerr != nil {
		return nil, rerr
	}
	return nil, ErrStoreNotFound
}

// GetPaymentPromise retrieves a [PaymentPromise] by its hash.
func (s *Store) GetPaymentPromise(_ context.Context, promiseHash []byte) (*PaymentPromise, error) {
	data, closer, err := s.db.Get(promiseKey(promiseHash))
	if err != nil {
		return nil, fmt.Errorf("getting payment promise: %w", err)
	}
	defer closer.Close()

	var ppProto types.PaymentPromise
	if err := gogoproto.Unmarshal(data, &ppProto); err != nil {
		return nil, fmt.Errorf("unmarshaling payment promise: %w", err)
	}

	var promise PaymentPromise
	if err := promise.FromProto(&ppProto); err != nil {
		return nil, fmt.Errorf("converting from proto: %w", err)
	}

	return &promise, nil
}

// PruneBefore deletes all shards and payment promises with pruneAt before the given time
// and returns the number of pruned entries.
//
// It works by iterating over the ordered prune index and deleting each entry until the given time,
// so it iterates exactly over the entries that need to be pruned. The order is guaranteed by the
// underlying database and enforced with query.OrderByKey{}.
func (s *Store) PruneBefore(_ context.Context, before time.Time) (int, error) {
	prefix := []byte("/prune/")
	iter, err := s.db.NewIter(&pebbledb.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return 0, fmt.Errorf("creating iterator: %w", err)
	}
	defer iter.Close()

	batch := s.db.NewBatch()
	defer batch.Close()

	pruned := 0
	beforeStr := formatTimestamp(before.UTC())
	for valid := iter.First(); valid; valid = iter.Next() {
		key := iter.Key()

		// Keys are sorted; once the timestamp reaches the cutoff we're done.
		keyStr := string(key)
		timestampStr := keyStr[7:19] // skip "/prune/" (7 chars), take YYYYMMDDHHmm
		if timestampStr >= beforeStr {
			break
		}

		commitment, promiseHash, ok := parsePruneKey(keyStr)
		if !ok {
			continue
		}

		// Missing file is fine (orphan marker from a crashed Put).
		if err := s.fs.Remove(s.shardFilePath(commitment, promiseHash)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return pruned, fmt.Errorf("removing shard file: %w", err)
		}

		if err := batch.Delete(key, pebbledb.NoSync); err != nil {
			return pruned, fmt.Errorf("deleting prune index: %w", err)
		}
		if err := batch.Delete(shardKey(commitment, promiseHash), pebbledb.NoSync); err != nil {
			return pruned, fmt.Errorf("deleting shard marker: %w", err)
		}
		if err := batch.Delete(promiseKey(promiseHash), pebbledb.NoSync); err != nil {
			return pruned, fmt.Errorf("deleting payment promise: %w", err)
		}
		pruned++
	}

	if err := iter.Error(); err != nil {
		return pruned, fmt.Errorf("iterating prune index: %w", err)
	}
	if err := batch.Commit(pebbledb.NoSync); err != nil {
		return pruned, fmt.Errorf("committing batch: %w", err)
	}
	return pruned, nil
}

// reconcile drops everything under <store>/staging/. Anything there at open
// time is a leftover from a Put that crashed before the rename. Orphan
// markers and orphan files in shards/ are intentionally not cleaned here:
// markers self-heal in [Store.Get] and at pruneAt via [Store.PruneBefore];
// rare orphan files (pebble.NoSync power loss after rename) are accepted.
func (s *Store) reconcile() error {
	start := time.Now()
	n, err := s.resetStaging()
	elapsedMs := time.Since(start).Milliseconds()
	if err != nil {
		s.log.Error("store reconcile failed", "error", err, "elapsed_ms", elapsedMs)
		return err
	}
	s.log.Info("store reconcile complete", "staging_files_removed", n, "elapsed_ms", elapsedMs)
	return nil
}

// resetStaging removes and recreates <store>/staging/, returning the number
// of entries that were dropped. A missing dir is treated as zero.
func (s *Store) resetStaging() (int, error) {
	stagingDir := filepath.Join(s.cfg.Path, stagingSubdir)
	entries, err := s.fs.List(stagingDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("reading staging: %w", err)
	}
	if err := s.fs.RemoveAll(stagingDir); err != nil {
		return len(entries), fmt.Errorf("removing staging: %w", err)
	}
	if err := s.fs.MkdirAll(stagingDir, 0o755); err != nil {
		return len(entries), fmt.Errorf("recreating staging: %w", err)
	}
	return len(entries), nil
}

// Close closes the underlying pebble database. For [NewMemoryStore] the
// in-memory FS is dropped when the Store is garbage collected.
func (s *Store) Close() error {
	return s.db.Close()
}

// formatTimestamp formats t with minute precision (YYYYMMDDHHmm) for
// lexicographic ordering in the prune index.
func formatTimestamp(timestamp time.Time) string {
	return timestamp.Format("200601021504")
}

func promiseKey(promiseHash []byte) []byte {
	return fmt.Appendf(nil, "/pp/%s", hex.EncodeToString(promiseHash))
}

func shardKey(commitment Commitment, promiseHash []byte) []byte {
	return fmt.Appendf(nil, "/shard/%s/%s", commitment.String(), hex.EncodeToString(promiseHash))
}

// pruneKey is keyed by pruneAt so [Store.PruneBefore] scans in timestamp
// order. Re-puts of the same (commit, promiseHash) are idempotent because
// pruneAt = CreationTimestamp + PaymentPromiseTimeout and CreationTimestamp
// is part of the hash; only a governance change to PaymentPromiseTimeout
// between two re-puts would split the key, and that case self-corrects when
// the stale entry fires.
func pruneKey(pruneAt time.Time, commitment Commitment, promiseHash []byte) []byte {
	return fmt.Appendf(nil, "/prune/%s/%s/%s", formatTimestamp(pruneAt.UTC()), commitment.String(), hex.EncodeToString(promiseHash))
}

// prefixUpperBound returns the upper bound for a prefix scan.
// It increments the last byte of the prefix to create an exclusive upper bound.
// For example, "/shard/abc" returns "/shard/abd".
func prefixUpperBound(prefix []byte) []byte {
	upper := make([]byte, len(prefix))
	copy(upper, prefix)
	for i := len(upper) - 1; i >= 0; i-- {
		upper[i]++
		if upper[i] != 0 {
			return upper
		}
	}
	// all 0xff bytes - return nil to indicate no upper bound
	return nil
}

// parsePruneKey extracts commitment and promise hash from a prune index key.
// Key format: /prune/<timestamp>/<commitment>/<promise-hash>
func parsePruneKey(key string) (Commitment, []byte, bool) {
	// split: ["", "prune", "<timestamp>", "<commitment>", "<promise-hash>"]
	parts := strings.Split(key, "/")
	if len(parts) != 5 {
		return Commitment{}, nil, false
	}

	commitment, err := CommitmentFromString(parts[3])
	if err != nil {
		return Commitment{}, nil, false
	}

	promiseHash, err := hex.DecodeString(parts[4])
	if err != nil {
		return Commitment{}, nil, false
	}

	return commitment, promiseHash, true
}
