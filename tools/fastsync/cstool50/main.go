// Command cstool50 applies recorded changesets to an app v4+ (SDK v0.50
// stores, IAVL v1) application DB and checks every height's app hash against
// fshdr's verified expected.tsv.
//
//	cstool50 apply  -db DATA_DIR -changesets DIR -expected expected.tsv -to H
//	                [-backend pebbledb] [-chain-id ID] [-parallel] [-allow-unverified]
//	cstool50 hashes -changesets DIR [-from A] [-to B]
//
// Each block's recorded store writes are applied in recorded order and
// committed; the resulting app hash must equal the AppHash of header h+1
// (column 2 of row h). A height with no row is not applied: the run stops
// before it. On a mismatch the height is rolled back and cstool50 exits 2.
// It continues a DB that cstool46 left at the v0.46 -> v0.50 handover: the
// first v0.50 block carries the store upgrade the v4 binary ran at load.
//
// hashes prints "height<TAB>recorded app hash" of the changesets, for
// comparisons; recorded hashes are not trusted.
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

	"cosmossdk.io/log"
	"cosmossdk.io/store/changeset"
	"cosmossdk.io/store/changeset/applier"
	"cosmossdk.io/store/rootmulti"
	dbm "github.com/cosmos/cosmos-db"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/expected"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "apply":
		os.Exit(exitCode(cmdApply(os.Args[2:], os.Stdout), os.Stderr))
	case "hashes":
		os.Exit(exitCode(cmdHashes(os.Args[2:], os.Stdout), os.Stderr))
	}
	usage()
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cstool50 apply|hashes [flags]  (-h for flags)")
	os.Exit(1)
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
		fmt.Fprintln(w, "cstool50: FAILED:", err)
		return 1
	}
}

func openDB(dir, backend string) (dbm.DB, error) {
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
	opts.Logger = log.NewNopLogger()
	opts.Logf = func(f string, a ...any) { fmt.Fprintf(out, f+"\n", a...) }
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
	fmt.Fprintf(out, "done: applied %d blocks (to height %d), %d ops in %.1fs; verified against trusted headers: %d, recorded hash only: %d\n",
		a.Applied, a.Latest, a.Ops, time.Since(start).Seconds(), a.CheckedExt, a.CheckedLocal)
	if a.Unverified > 0 {
		fmt.Fprintf(out, "WARNING: %d heights had no hash to verify against\n", a.Unverified)
	}
	if *to > 0 && a.Latest < *to {
		fmt.Fprintf(out, "stopped before height %d: no trusted app hash or no recorded block for it\n", a.Latest+1)
	}
	return nil
}

var errStop = errors.New("stop")

// cmdHashes prints the recorded app hash of every block in [from, to].
func cmdHashes(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("hashes", flag.ContinueOnError)
	csDir := fs.String("changesets", "", "changeset directory")
	from := fs.Int64("from", 0, "first height")
	to := fs.Int64("to", 0, "last height (0 = all)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *csDir == "" {
		return errors.New("-changesets is required")
	}
	err := changeset.Stream(*csDir, *from, func(b *changeset.Block) error {
		if *to != 0 && b.Height > *to {
			return errStop
		}
		_, err := fmt.Fprintf(out, "%d\t%X\n", b.Height, b.AppHash)
		return err
	})
	if errors.Is(err, errStop) {
		return nil
	}
	return err
}
