// Command cstool46 applies recorded changesets to an app v1-v3 (SDK v0.46,
// IAVL v0.19) application DB and checks every height's app hash against
// fshdr's verified expected.tsv.
//
//	cstool46 apply -db DATA_DIR -changesets DIR -expected expected.tsv -to H
//	               [-backend pebbledb] [-chain-id ID] [-parallel] [-allow-unverified]
//
// Each block's recorded store writes are applied in recorded order and
// committed; the resulting app hash must equal the AppHash of header h+1
// (column 2 of row h). A height with no row is not applied: the run stops
// before it. On a mismatch the height is rolled back and cstool46 exits 2.
// Blocks recorded by the SDK v0.50 store (app v4+) end the run with a
// HANDOVER line; cstool50 continues on the same DB.
//
// The last line is "done: ... (to height N) ...; verified against trusted
// headers: X, recorded hash only: Y"; Y is 0 unless -allow-unverified.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/store/changeset"
	"github.com/cosmos/cosmos-sdk/store/changeset/applier"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	tmlog "github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/expected"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "apply" {
		fmt.Fprintln(os.Stderr, "usage: cstool46 apply [flags]  (-h for flags)")
		os.Exit(1)
	}
	os.Exit(exitCode(cmdApply(os.Args[2:], os.Stdout), os.Stderr))
}

// exitCode reports err and maps an app hash mismatch to 2.
func exitCode(err error, w io.Writer) int {
	var mm *applier.MismatchError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &mm):
		bar := strings.Repeat("!", 64)
		fmt.Fprintf(w, "\n%s\nAPP HASH MISMATCH: %v\n%s\n", bar, err, bar)
		return 2
	default:
		fmt.Fprintln(w, "cstool46: FAILED:", err)
		return 1
	}
}

func openDB(dir, backend string) (dbm.DB, error) {
	if backend == string(PebbleDBBackend) {
		return NewPebbleDB("application", dir)
	}
	return dbm.NewDB("application", dbm.BackendType(backend), dir)
}

// chainID returns flagValue, or the chain id in the genesis of the node home
// that holds dataDir (DATA_DIR/../config/genesis.json).
func chainID(flagValue, dataDir string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	p := filepath.Join(filepath.Dir(filepath.Clean(dataDir)), "config", "genesis.json")
	bz, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("no -chain-id and no genesis at %s: %w", p, err)
	}
	return expected.GenesisChainID(bz)
}

func cmdApply(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	dbDir := fs.String("db", "", "directory containing application.db (node stopped)")
	backend := fs.String("backend", "goleveldb", "db backend: goleveldb|pebbledb")
	csDir := fs.String("changesets", "", "changeset directory")
	expFile := fs.String("expected", "", "fshdr expected.tsv verified for this chain")
	chain := fs.String("chain-id", "", "chain id expected.tsv must be verified for (default: from DATA_DIR/../config/genesis.json)")
	to := fs.Int64("to", 0, "stop after this height (0 = all)")
	_ = fs.Bool("require-trusted", true, "every height must match expected.tsv (always on unless -allow-unverified)")
	allowUnverified := fs.Bool("allow-unverified", false, "DANGEROUS: accept heights without a trusted app hash (checked against the recorded hash only)")
	cacheSize := fs.Int("iavl-cache", 500000, "IAVL node cache size per store")
	parallel := fs.Bool("parallel", false, "apply the stores of a block concurrently")
	every := fs.Int64("progress", 1000, "print progress every N blocks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbDir == "" || *csDir == "" || (*expFile == "" && !*allowUnverified) {
		return errors.New("need -db, -changesets and -expected")
	}
	var trusted func(int64) ([]byte, bool, error)
	if *expFile != "" {
		id, err := chainID(*chain, *dbDir)
		if err != nil {
			return err
		}
		r, err := expected.Open(*expFile, id)
		if err != nil {
			return err
		}
		defer r.Close()
		trusted = r.AppHash
		fmt.Fprintf(out, "trusted app hashes: %s (chain %s)\n", *expFile, id)
	}
	if *allowUnverified {
		fmt.Fprintln(out, "WARNING: -allow-unverified: heights without a trusted app hash are applied against the recorded hash only; the result is NOT verified")
	}
	db, err := openDB(*dbDir, *backend)
	if err != nil {
		return err
	}
	defer db.Close()

	if cur := rootmulti.GetLatestVersion(db); *to > 0 && cur >= *to {
		fmt.Fprintf(out, "done: applied 0 blocks (to height %d), the DB was already there; verified against trusted headers: 0, recorded hash only: 0\n", cur)
		return nil
	}
	opts := applier.DefaultOptions()
	opts.IAVLCacheSize = *cacheSize
	opts.Parallel = *parallel
	opts.AllowUnverified = *allowUnverified
	opts.Logger = tmlog.NewNopLogger()
	start, last := time.Now(), time.Now()
	var lastN int64
	a, err := applier.Run(db, *csDir, *to, opts, trusted, func(a *applier.Applier, b *changeset.Block) {
		if b.Upgrade != nil {
			fmt.Fprintf(out, "height %d: store upgrade added=%v deleted=%v renamed=%v\n", b.Height, b.Upgrade.Added, b.Upgrade.Deleted, b.Upgrade.Renamed)
		}
		if *every > 0 && a.Applied%*every == 0 {
			dt := time.Since(last).Seconds()
			fmt.Fprintf(out, "height %d applied=%d %.1f blocks/s\n", b.Height, a.Applied, float64(a.Applied-lastN)/dt)
			last, lastN = time.Now(), a.Applied
		}
	})
	if err != nil {
		fmt.Fprintf(out, "stopped: the DB is at height %d\n", rootmulti.GetLatestVersion(db))
		return err
	}
	if a == nil { // nothing to apply
		a = &applier.Applier{Latest: rootmulti.GetLatestVersion(db)}
	}
	fmt.Fprintf(out, "done: applied %d blocks (to height %d), %d ops in %.1fs; verified against trusted headers: %d, recorded hash only: %d\n",
		a.Applied, a.Latest, a.Ops, time.Since(start).Seconds(), a.CheckedExt, a.CheckedLocal)
	if a.Unverified > 0 {
		fmt.Fprintf(out, "WARNING: %d heights had no hash to verify against\n", a.Unverified)
	}
	if a.WaitingFor > 0 {
		fmt.Fprintf(out, "stopped before height %d: expected.tsv has no row for it (extend fshdr's range)\n", a.WaitingFor)
	}
	if a.HandoverAt > 0 {
		fmt.Fprintf(out, "HANDOVER: block %d was recorded by the SDK v0.50 store (app v4+); the DB is at %d. Continue with cstool50 apply on the same DB.\n", a.HandoverAt, a.Latest)
	}
	return nil
}
