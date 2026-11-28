// Command bsfix repairs a CometBFT block store that is ahead of the state store.
//
// After an unclean stop, a node whose block sync pipelines block saving ahead of
// block execution can hold blocks above state height + 1 (for example block S+2
// saved while the state is at S). The node then refuses to start. bsfix deletes
// the latest blocks until the block store height is at most state height + 1.
//
// The node must be stopped. bsfix never touches the state store or the
// application database.
//
//	bsfix -data DIR [-backend pebbledb] [-dry-run]
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	dbm "github.com/cometbft/cometbft-db"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "bsfix:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("bsfix", flag.ContinueOnError)
	dir := fs.String("data", "", "node data directory (holds blockstore.db and state.db)")
	backend := fs.String("backend", "pebbledb", "database backend (goleveldb or pebbledb)")
	dry := fs.Bool("dry-run", false, "print what would be deleted, change nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("-data is required")
	}
	for _, name := range []string{"blockstore.db", "state.db"} {
		if _, err := os.Stat(fmt.Sprintf("%s/%s", *dir, name)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	bdb, err := dbm.NewDB("blockstore", dbm.BackendType(*backend), *dir)
	if err != nil {
		return fmt.Errorf("open blockstore (is the node stopped?): %w", err)
	}
	sdb, err := dbm.NewDB("state", dbm.BackendType(*backend), *dir)
	if err != nil {
		_ = bdb.Close()
		return fmt.Errorf("open state (is the node stopped?): %w", err)
	}
	bs := store.NewBlockStore(bdb)
	ss := sm.NewStore(sdb, sm.StoreOptions{})
	err = trim(bs, ss, *dry, out)
	return errors.Join(err, bs.Close(), ss.Close())
}

// trim deletes the latest blocks of bs until bs.Height() <= state height + 1.
func trim(bs *store.BlockStore, ss sm.Store, dry bool, out io.Writer) error {
	st, err := ss.Load()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if st.IsEmpty() {
		return errors.New("state store is empty")
	}
	limit := st.LastBlockHeight + 1
	fmt.Fprintf(out, "blockstore base=%d height=%d, state height=%d\n", bs.Base(), bs.Height(), st.LastBlockHeight)
	if bs.Height() <= limit {
		fmt.Fprintln(out, "nothing to do")
		return nil
	}
	if dry {
		fmt.Fprintf(out, "would delete blocks %d..%d\n", limit+1, bs.Height())
		return nil
	}
	for bs.Height() > limit {
		h := bs.Height()
		if err := bs.DeleteLatestBlock(); err != nil {
			return fmt.Errorf("delete block %d: %w", h, err)
		}
		if bs.Height() >= h {
			return fmt.Errorf("delete block %d: height did not decrease", h)
		}
		fmt.Fprintf(out, "deleted block %d -> blockstore height %d\n", h, bs.Height())
	}
	return nil
}
