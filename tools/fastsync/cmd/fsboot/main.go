// Command fsboot bootstraps a node's CometBFT stores at height H offline,
// from data it checks against the verified hash of header H+1. It writes the
// same bytes as `celestia-appd comet bootstrap-state`, without a light client
// or a trusting period, so it works at any height that expected.tsv covers.
//
//	fsboot bootstrap -home DIR -height H (-expected expected.tsv | -trusted-hash HEX)
//	                 (-rpc URL[,URL...] | -src-data DIR [-src-backend B]) [-app-hash HEX] [-dry-run]
//
// The only trusted input is the hash of header H+1: row H, column 3 of
// fshdr's expected.tsv, or -trusted-hash. Headers, validator sets, the commit
// and the consensus params come from an untrusted source and are checked
// against it (see internal/boot.Verify).
//
// Not authenticated by any header, as in the stock command: proposer
// priorities and the consensus params other than block max bytes and max
// gas. With several -rpc servers, all of them must return identical values.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	cfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/spf13/viper"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/boot"
	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/expected"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "bootstrap":
		err = cmdBootstrap(os.Args[2:], os.Stdout)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fsboot: FAILED:", err)
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: fsboot bootstrap [flags]  (-h for flags)")
	os.Exit(1)
}

// loadConfig reads <home>/config/config.toml as the node does (defaults, then the file).
func loadConfig(home string) (*cfg.Config, error) {
	if home == "" {
		return nil, errors.New("-home is required")
	}
	conf := cfg.DefaultConfig()
	v := viper.New()
	v.SetConfigFile(filepath.Join(home, "config", "config.toml"))
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := v.Unmarshal(conf); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	conf.SetRoot(home)
	return conf, nil
}

// genesisProvider reads the genesis file the way celestia-appd's start and
// bootstrap-state commands do (the SDK's AppGenesis, which also accepts a
// CometBFT genesis), so the stored genesis doc is the same.
func genesisProvider(conf *cfg.Config) node.GenesisDocProvider {
	return func() (*types.GenesisDoc, error) {
		ag, err := genutiltypes.AppGenesisFromFile(conf.GenesisFile())
		if err != nil {
			return nil, err
		}
		return ag.ToGenesisDoc()
	}
}

func cmdBootstrap(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	home := fs.String("home", "", "node home (config/config.toml, config/genesis.json; data dir from the config)")
	height := fs.Int64("height", 0, "height H: the application DB holds the state after H")
	exp := fs.String("expected", "", "fshdr expected.tsv: row H column 3 is the verified hash of header H+1")
	trusted := fs.String("trusted-hash", "", "verified hash of header H+1 (hex), instead of -expected")
	rpcs := fs.String("rpc", "", "comma-separated RPC servers (untrusted source)")
	srcData := fs.String("src-data", "", "untrusted source: data dir of a stopped node that synced past H+2")
	srcBackend := fs.String("src-backend", "", "db backend of -src-data (default: this node's db_backend)")
	appHash := fs.String("app-hash", "", "optional: app hash of the local application DB at H (must equal header(H+1).AppHash)")
	dry := fs.Bool("dry-run", false, "verify everything, write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *height <= 0 || (*exp == "") == (*trusted == "") || (*rpcs == "") == (*srcData == "") {
		return errors.New("need -home, -height, exactly one of -expected/-trusted-hash and exactly one of -rpc/-src-data")
	}
	h := *height
	logf := func(format string, a ...any) {
		fmt.Fprintf(out, "[fsboot %s] %s\n", time.Now().UTC().Format("15:04:05"), fmt.Sprintf(format, a...))
	}
	t0 := time.Now()
	conf, err := loadConfig(*home)
	if err != nil {
		return err
	}
	genDoc, err := genesisProvider(conf)()
	if err != nil {
		return fmt.Errorf("genesis: %w", err)
	}

	var trustedHash, wantAppHash []byte
	if *exp != "" {
		row, err := expected.Find(*exp, h)
		if err != nil {
			return fmt.Errorf("%s: %w", *exp, err)
		}
		trustedHash, wantAppHash = row.NextHeaderHash, row.AppHash
	} else if trustedHash, err = hex.DecodeString(*trusted); err != nil || len(trustedHash) != 32 {
		return errors.New("-trusted-hash: want 32 bytes of hex")
	}
	logf("chain %s, H=%d, trusted hash(header %d) = %X", genDoc.ChainID, h, h+1, trustedHash)

	var src boot.Source
	if *rpcs != "" {
		src, err = boot.NewRPCSource(genDoc.ChainID, strings.Split(*rpcs, ","))
	} else {
		b := *srcBackend
		if b == "" {
			b = conf.DBBackend
		}
		src, err = boot.OpenDataSource(*srcData, b)
	}
	if err != nil {
		return err
	}
	defer src.Close()
	logf("source: %s (untrusted; every item is checked)", src)

	v, err := boot.Verify(context.Background(), src, genDoc.ChainID, h, trustedHash)
	if err != nil {
		return err
	}
	if wantAppHash != nil && !strings.EqualFold(fmt.Sprintf("%X", v.Next.AppHash), fmt.Sprintf("%X", wantAppHash)) {
		return fmt.Errorf("header %d: AppHash %X != expected.tsv %X", h+1, v.Next.AppHash, wantAppHash)
	}
	if *appHash != "" && !strings.EqualFold(*appHash, fmt.Sprintf("%X", v.Next.AppHash)) {
		return fmt.Errorf("app hash of the local DB %s != header(%d).AppHash %X", *appHash, h+1, v.Next.AppHash)
	}
	logf("ok headers %d/%d, validator sets %d/%d/%d (%d/%d/%d validators), consensus params %d", h, h+1, h, h+1, h+2,
		v.Vals[0].Size(), v.Vals[1].Size(), v.Vals[2].Size(), h+1)
	if v.Canonical {
		logf("ok commit %d: all signatures valid, +2/3 of validators %d, canonical (header(%d).LastCommitHash)", h, h, h+1)
	} else {
		logf("ok commit %d: all signatures valid, +2/3 of validators %d (a valid seen commit, not block %d's LastCommit)", h, h, h+1)
	}
	if *dry {
		logf("dry run: all checks passed, nothing written (%s)", time.Since(t0).Round(time.Millisecond))
		return nil
	}
	if err := boot.Write(conf, genesisProvider(conf), v); err != nil {
		return err
	}
	logf("DONE: state at height %d (app version %d, AppHash %X), seen commit %d, offline state sync height %d (%s)",
		h, v.Next.Version.App, v.Next.AppHash, h, h, time.Since(t0).Round(time.Millisecond))
	return nil
}
