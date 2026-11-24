// Command fastindex46 builds (or checks) the IAVL fast-node index of every
// store of an SDK v0.46 application DB (app v1 to v3, IAVL v0.19), offline.
//
// The changeset appliers write with the fast-node index off, so IAVL leaves
// it absent or stale (storage version other than "1.1.0-<latest version>").
// A node with iavl-disable-fastnode=false (the default) then rebuilds it for
// every store inside LoadVersion at its first start, before it does anything
// else: minutes or more on mainnet-size state, and silent at log_level=warn.
//
// fastindex46 opens the DB as the node does (rootmulti, IAVL stores mounted from
// the latest commit info, fast node on), so IAVL builds the index here; then
// it checks that the commit id is unchanged (the index is not part of any
// hash) and that no store needs a rebuild any more.
//
//	fastindex46 -db DATA_DIR [-backend pebbledb] [-check]
//
// With -check it only reports, and exits 3 if a node would rebuild the index.
// For SDK v0.50 DBs (app v4 and later) use cmd/fastindex in the parent module.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/tendermint/tendermint/libs/log"

	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	dbm "github.com/tendermint/tm-db"
)

// errNeedsRebuild is returned by -check when a node would rebuild the index.
var errNeedsRebuild = errors.New("a node would rebuild the fast-node index of some stores at its first start")

func main() {
	err := run(os.Args[1:], os.Stdout)
	switch {
	case errors.Is(err, errNeedsRebuild):
		fmt.Fprintln(os.Stderr, "fastindex46:", err)
		os.Exit(3)
	case err != nil:
		fmt.Fprintln(os.Stderr, "fastindex46: FAILED:", err)
		os.Exit(2)
	}
}

type storeIndex struct {
	name, storageVersion string
	fastNodes            int64
	empty                bool // no tree nodes: nothing to index
}

// indexStatus reads each store's IAVL metadata and counts its fast nodes.
func indexStatus(db dbm.DB, names []string) ([]storeIndex, error) {
	var out []storeIndex
	for _, n := range names {
		p := "s/k:" + n + "/"
		v, err := db.Get([]byte(p + "m" + "storage_version"))
		if err != nil {
			return nil, err
		}
		fast, err := count(db, p+"f", p+"g", -1)
		if err != nil {
			return nil, err
		}
		nodes, err := count(db, p+"n", p+"o", 1) // IAVL v0.19 nodes
		if err != nil {
			return nil, err
		}
		empty := nodes == 0
		out = append(out, storeIndex{n, string(v), fast, empty})
	}
	return out, nil
}

// openDB opens application.db as an app v1 to v3 node does.
func openDB(dir, backend string) (dbm.DB, error) {
	if backend == string(PebbleDBBackend) {
		return NewPebbleDB("application", dir)
	}
	return dbm.NewDB("application", dbm.BackendType(backend), dir)
}

// count counts keys in [start, end), stopping at limit (if >= 0).
func count(db dbm.DB, start, end string, limit int64) (int64, error) {
	it, err := db.Iterator([]byte(start), []byte(end))
	if err != nil {
		return 0, err
	}
	defer it.Close()
	var c int64
	for ; it.Valid() && (limit < 0 || c < limit); it.Next() {
		c++
	}
	return c, it.Error()
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("fastindex46", flag.ContinueOnError)
	dbDir := fs.String("db", "", "directory containing application.db (node stopped)")
	backend := fs.String("backend", "goleveldb", "db backend")
	cacheSize := fs.Int("iavl-cache", 500000, "IAVL node cache size per store")
	check := fs.Bool("check", false, "only report; exit 3 if a node would rebuild the index")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbDir == "" {
		return errors.New("-db is required")
	}
	if _, err := os.Stat(*dbDir + "/application.db"); err != nil {
		return err
	}
	db, err := openDB(*dbDir, *backend)
	if err != nil {
		return err
	}
	defer db.Close()

	latest := rootmulti.GetLatestVersion(db)
	if latest == 0 {
		return fmt.Errorf("no committed version in %s", *dbDir)
	}
	bz, err := db.Get(fmt.Appendf(nil, "s/%d", latest))
	if err != nil || bz == nil {
		return fmt.Errorf("no commit info for version %d", latest)
	}
	var ci storetypes.CommitInfo
	if err := ci.Unmarshal(bz); err != nil {
		return err
	}
	var names []string
	for _, si := range ci.StoreInfos {
		if si.CommitId.Version == 0 { // memory and transient stores: nothing on disk
			continue
		}
		names = append(names, si.Name)
	}
	sort.Strings(names)
	want := fmt.Sprintf("1.1.0-%d", latest)
	report := func(tag string) (int, error) {
		st, err := indexStatus(db, names)
		if err != nil {
			return 0, err
		}
		stale, total := 0, int64(0)
		parts := make([]string, 0, len(st))
		for _, s := range st {
			if s.storageVersion != want && !s.empty {
				stale++
			}
			total += s.fastNodes
			e := ""
			if s.empty {
				e = "(empty)"
			}
			parts = append(parts, fmt.Sprintf("%s=%q/%d%s", s.name, s.storageVersion, s.fastNodes, e))
		}
		fmt.Fprintf(out, "%s: version %d, %d IAVL stores, %d non-empty stores need a rebuild (storage version != %q), %d fast nodes\n  %s\n",
			tag, latest, len(names), stale, want, total, strings.Join(parts, " "))
		return stale, nil
	}
	stale, err := report("before")
	if err != nil {
		return err
	}
	if *check {
		if stale > 0 {
			return errNeedsRebuild
		}
		return nil
	}

	rs := rootmulti.NewStore(db, log.NewNopLogger())
	rs.SetPruning(pruningtypes.NewPruningOptions(pruningtypes.PruningNothing))
	rs.SetIAVLCacheSize(*cacheSize)
	rs.SetIAVLDisableFastNode(false) // what the node does
	for _, n := range names {
		rs.MountStoreWithDB(storetypes.NewKVStoreKey(n), storetypes.StoreTypeIAVL, nil)
	}
	t0 := time.Now()
	if err := rs.LoadLatestVersion(); err != nil { // IAVL builds the index of every stale store here
		return err
	}
	got := rs.LastCommitID()
	if got.Version != latest || !bytes.Equal(got.Hash, ci.CommitID().Hash) {
		return fmt.Errorf("commit id changed: %d/%X, was %d/%X", got.Version, got.Hash, latest, ci.CommitID().Hash)
	}
	fmt.Fprintf(out, "loaded with fast node on in %s; commit id unchanged (%d/%X)\n", time.Since(t0).Round(time.Millisecond), got.Version, got.Hash)
	if stale, err = report("after"); err != nil {
		return err
	}
	if stale > 0 {
		return fmt.Errorf("%d stores still without a current fast-node index", stale)
	}
	return nil
}
