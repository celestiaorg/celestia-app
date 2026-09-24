// Command scan-blockresults reports the heights whose FinalizeBlock responses, the data
// behind /block_results, are missing from a node's state.db, grouped into gaps with the app
// version of their blocks. It opens the stores read-only.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"

	"github.com/cockroachdb/pebble"
	dbm "github.com/cometbft/cometbft-db"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// maxReportedVerifyFailures caps how many verification failures are printed individually.
const maxReportedVerifyFailures = 20

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	err := Run(ctx)
	cancel()
	if err != nil {
		log.Fatalln("ERR:", err)
	}
}

func Run(ctx context.Context) error {
	home := flag.String("home", "", "node home directory")
	dbDir := flag.String("db-dir", "", "directory with state.db (default: <home>/data)")
	blockstoreDir := flag.String("blockstore-dir", "", "directory with blockstore.db (default: --db-dir)")
	backend := flag.String("backend", string(dbm.PebbleDBBackend), "db backend: pebbledb or goleveldb")
	from := flag.Int64("from", 0, "first height to scan (default: blockstore base)")
	to := flag.Int64("to", 0, "last height to scan (default: blockstore height)")
	verifyEvery := flag.Int64("verify-every", 0, "also verify every K-th present result against block H+1 (0 = off)")
	flag.Parse()

	if *dbDir == "" {
		if *home == "" {
			return errors.New("--home or --db-dir is required")
		}
		*dbDir = filepath.Join(*home, "data")
	}
	if *blockstoreDir == "" {
		*blockstoreDir = *dbDir
	}

	blockStoreDB, err := openReadOnly("blockstore", dbm.BackendType(*backend), *blockstoreDir)
	if err != nil {
		return err
	}
	defer blockStoreDB.Close()

	stateDB, err := openReadOnly("state", dbm.BackendType(*backend), *dbDir)
	if err != nil {
		return err
	}
	defer stateDB.Close()

	return Scan(ctx, store.NewBlockStore(blockStoreDB), stateDB, *from, *to, *verifyEvery)
}

// openReadOnly opens an existing store without the risk of writing to it: a read-write open can
// trigger compactions, and would create an empty store if the path were wrong.
func openReadOnly(name string, backend dbm.BackendType, dir string) (dbm.DB, error) {
	var (
		db  dbm.DB
		err error
	)
	switch backend {
	case dbm.PebbleDBBackend:
		db, err = dbm.NewPebbleDBWithOpts(name, dir, &pebble.Options{ReadOnly: true})
	case dbm.GoLevelDBBackend:
		db, err = dbm.NewGoLevelDBWithOpts(name, dir, &opt.Options{ReadOnly: true, ErrorIfMissing: true})
	default:
		return nil, fmt.Errorf("unsupported backend %q", backend)
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s.db in %s: %w", name, dir, err)
	}
	return db, nil
}

func Scan(ctx context.Context, blockStore *store.BlockStore, stateDB dbm.DB, from, to, verifyEvery int64) error {
	base, height := blockStore.Base(), blockStore.Height()
	fmt.Printf("blockstore: base=%d height=%d\n", base, height)
	if height == 0 {
		return errors.New("blockstore is empty")
	}
	if from < base || from == 0 {
		from = base
	}
	if to > height || to == 0 {
		to = height
	}
	if from > to {
		return fmt.Errorf("nothing to scan: from=%d > to=%d", from, to)
	}
	fmt.Printf("scanning heights [%d, %d]\n\n", from, to)

	stateStore := sm.NewStore(stateDB, sm.StoreOptions{})
	var (
		gaps                     gapCollector
		verified, verifyFailures int64
	)
	for h := from; h <= to; h++ {
		if h%10_000 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}

		// checking the raw key avoids unmarshalling every response
		buf, err := stateDB.Get(abciResponsesKey(h))
		if err != nil {
			return fmt.Errorf("reading result at %d: %w", h, err)
		}
		if len(buf) == 0 {
			var appVersion uint64
			meta := blockStore.LoadBlockMeta(h)
			if meta != nil {
				appVersion = meta.Header.Version.App
			}
			gaps.add(h, appVersion, meta == nil)
			continue
		}

		if verifyEvery > 0 && h%verifyEvery == 0 {
			verified++
			if err := verifyResult(stateStore, blockStore, h); err != nil {
				verifyFailures++
				if verifyFailures <= maxReportedVerifyFailures {
					fmt.Printf("verify failed at %d: %v\n", h, err)
				}
			}
		}
	}

	if verifyEvery > 0 {
		fmt.Printf("verified %d present results, %d failed\n\n", verified, verifyFailures)
	}
	printGaps(gaps.gaps)
	if verifyFailures > 0 {
		return fmt.Errorf("%d present results failed verification", verifyFailures)
	}
	return nil
}

// verifyResult loads the result the way /block_results does and checks it against the hashes
// committed in block H+1. Results stored in the legacy format carry no AppHash and are only
// checked for being loadable.
func verifyResult(stateStore sm.Store, blockStore *store.BlockStore, h int64) error {
	resp, err := stateStore.LoadFinalizeBlockResponse(h)
	if err != nil {
		return fmt.Errorf("not servable: %w", err)
	}
	if resp.AppHash == nil || h == blockStore.Height() {
		return nil
	}
	next := blockStore.LoadBlockMeta(h + 1)
	if next == nil {
		return fmt.Errorf("block %d is missing", h+1)
	}
	if !bytes.Equal(resp.AppHash, next.Header.AppHash) {
		return fmt.Errorf("AppHash %X does not match block %d AppHash %X", resp.AppHash, h+1, next.Header.AppHash)
	}
	if got := sm.TxResultsHash(resp.TxResults); !bytes.Equal(got, next.Header.LastResultsHash) {
		return fmt.Errorf("results hash %X does not match block %d LastResultsHash %X", got, h+1, next.Header.LastResultsHash)
	}
	return nil
}

// gap is a contiguous range of heights with missing results whose blocks share an app version.
type gap struct {
	start, end   int64
	appVersion   uint64
	blockMissing bool
}

func (g gap) len() int64 { return g.end - g.start + 1 }

type gapCollector struct {
	gaps []gap
}

func (c *gapCollector) add(h int64, appVersion uint64, blockMissing bool) {
	if n := len(c.gaps); n > 0 {
		last := &c.gaps[n-1]
		if last.end == h-1 && last.appVersion == appVersion && last.blockMissing == blockMissing {
			last.end = h
			return
		}
	}
	c.gaps = append(c.gaps, gap{start: h, end: h, appVersion: appVersion, blockMissing: blockMissing})
}

func printGaps(gaps []gap) {
	if len(gaps) == 0 {
		fmt.Println("no gaps: every height in range has a stored result")
		return
	}

	var total int64
	backfill := map[uint64]int64{}
	fmt.Printf("%-25s %-10s %-12s %s\n", "missing heights", "count", "app_version", "note")
	for _, g := range gaps {
		total += g.len()
		note := ""
		if g.blockMissing {
			note = "block also missing"
		} else {
			backfill[g.appVersion] += g.len()
		}
		fmt.Printf("%-25s %-10d %-12d %s\n", fmt.Sprintf("[%d - %d]", g.start, g.end), g.len(), g.appVersion, note)
	}
	fmt.Printf("\ntotal missing results: %d across %d gap(s)\n", total, len(gaps))

	fmt.Println("\napp versions required for backfill:")
	for _, v := range slices.Sorted(maps.Keys(backfill)) {
		fmt.Printf("  app_version=%d : %d heights\n", v, backfill[v])
	}
}

// abciResponsesKey mirrors calcABCIResponsesKey in cometbft's state/store.go.
func abciResponsesKey(height int64) []byte {
	return fmt.Appendf(nil, "abciResponsesKey:%v", height)
}
