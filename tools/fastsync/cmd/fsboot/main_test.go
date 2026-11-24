package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbm "github.com/cometbft/cometbft-db"
	cfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/node"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/testchain"
)

const (
	chainLen = 30
	bootH    = 12
)

// newHome writes a node home with the chain's genesis and returns its config.
func newHome(t *testing.T, c *testchain.Chain) *cfg.Config {
	t.Helper()
	conf := cfg.DefaultConfig()
	conf.SetRoot(t.TempDir())
	conf.DBBackend = "goleveldb"
	require.NoError(t, os.MkdirAll(filepath.Join(conf.RootDir, "config"), 0o755))
	require.NoError(t, os.MkdirAll(conf.DBDir(), 0o755))
	require.NoError(t, c.GenDoc.SaveAs(conf.GenesisFile()))
	cfg.WriteConfigFile(filepath.Join(conf.RootDir, "config", "config.toml"), conf)
	return conf
}

// dump returns every key and value of the home's state.db and blockstore.db.
func dump(t *testing.T, conf *cfg.Config) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range []string{"state", "blockstore"} {
		db, err := dbm.NewDB(name, dbm.GoLevelDBBackend, conf.DBDir())
		require.NoError(t, err)
		it, err := db.Iterator(nil, nil)
		require.NoError(t, err)
		for ; it.Valid(); it.Next() {
			out[name+"/"+string(it.Key())] = fmt.Sprintf("%X", it.Value())
		}
		require.NoError(t, it.Close())
		require.NoError(t, db.Close())
	}
	return out
}

func run(t *testing.T, conf *cfg.Config, extra ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	args := append([]string{"-home", conf.RootDir, "-height", fmt.Sprint(bootH)}, extra...)
	err := cmdBootstrap(args, &out)
	return out.String(), err
}

func trustedHash(c *testchain.Chain) string { return fmt.Sprintf("%X", c.Header(bootH+1).Hash()) }

// TestEquivalentToStockBootstrap runs celestia-core's BootstrapStateWithGenProvider
// (what `comet bootstrap-state` runs) and fsboot against the same RPC server and
// requires byte-identical state and block stores. fsboot from a data dir must
// produce the same bytes.
func TestEquivalentToStockBootstrap(t *testing.T) {
	c := testchain.New(t, chainLen)
	srv := c.ServeRPC(t, nil)

	stock := newHome(t, c)
	stock.StateSync.RPCServers = []string{srv.URL, srv.URL}
	stock.StateSync.TrustHeight = chainLen
	stock.StateSync.TrustHash = fmt.Sprintf("%X", c.Header(chainLen).Hash())
	stock.StateSync.TrustPeriod = 24 * time.Hour
	require.NoError(t, node.BootstrapStateWithGenProvider(context.Background(), stock, cfg.DefaultDBProvider,
		genesisProvider(stock), bootH, nil))
	want := dump(t, stock)
	for _, k := range []string{"state/stateKey", "state/genesisDoc", "state/offlineStateSyncHeightKey", "blockstore/SC:12",
		"state/validatorsKey:12", "state/validatorsKey:13", "state/validatorsKey:14", "state/consensusParamsKey:13"} {
		require.Contains(t, want, k)
	}

	home := newHome(t, c)
	out, err := run(t, home, "-trusted-hash", trustedHash(c), "-rpc", srv.URL)
	require.NoError(t, err, out)
	require.Contains(t, out, "canonical")
	require.Equal(t, want, dump(t, home), "fsboot (rpc) differs from bootstrap-state")

	src := t.TempDir()
	c.WriteStores(t, src, chainLen, chainLen-1)
	home2 := newHome(t, c)
	out, err = run(t, home2, "-trusted-hash", trustedHash(c), "-src-data", src)
	require.NoError(t, err, out)
	require.Equal(t, want, dump(t, home2), "fsboot (data dir) differs from bootstrap-state")

	// a second bootstrap into the same home is refused, as in stock
	_, err = run(t, home, "-trusted-hash", trustedHash(c), "-rpc", srv.URL)
	require.ErrorContains(t, err, "state not empty")
}

func TestExpectedFile(t *testing.T) {
	c := testchain.New(t, chainLen)
	srv := c.ServeRPC(t, nil)
	exp := filepath.Join(t.TempDir(), "expected.tsv")
	row := fmt.Sprintf("%d\t%X\t%X\t%X\t%X\t%X\t%d\n", bootH, c.Header(bootH+1).AppHash, c.Header(bootH+1).Hash(),
		c.Header(bootH+1).LastResultsHash, c.Header(bootH+1).ValidatorsHash, c.Header(bootH+1).NextValidatorsHash, testchain.AppVersion)
	require.NoError(t, os.WriteFile(exp, []byte("# test\n"+row), 0o644))
	out, err := run(t, newHome(t, c), "-expected", exp, "-rpc", srv.URL, "-dry-run")
	require.NoError(t, err, out)
	require.Contains(t, out, "dry run")

	bad := strings.Replace(row, fmt.Sprintf("%X", c.Header(bootH+1).AppHash), strings.Repeat("AB", 32), 1)
	require.NoError(t, os.WriteFile(exp, []byte(bad), 0o644))
	_, err = run(t, newHome(t, c), "-expected", exp, "-rpc", srv.URL, "-dry-run")
	require.ErrorContains(t, err, "!= expected.tsv")
}

// TestForgedSource covers the forgeries an untrusted RPC can attempt.
func TestForgedSource(t *testing.T) {
	c := testchain.New(t, chainLen)
	good := c.ServeRPC(t, nil)
	forgeAt := func(method string, f func(any)) testchain.Forge {
		return func(m string, res any) {
			if m == method {
				f(res)
			}
		}
	}
	valPower := forgeAt("validators", func(r any) { r.(*coretypes.ResultValidators).Validators[0].VotingPower++ })
	priority := forgeAt("validators", func(r any) { r.(*coretypes.ResultValidators).Validators[0].ProposerPriority += 12345 })
	maxBytes := forgeAt("consensus_params", func(r any) { r.(*coretypes.ResultConsensusParams).ConsensusParams.Block.MaxBytes++ })
	evidence := forgeAt("consensus_params", func(r any) {
		r.(*coretypes.ResultConsensusParams).ConsensusParams.Evidence.MaxAgeNumBlocks++
	})
	commitSig := forgeAt("commit", func(r any) {
		rc := r.(*coretypes.ResultCommit)
		sigs := append(rc.Commit.Signatures[:0:0], rc.Commit.Signatures...)
		sig := append([]byte(nil), sigs[0].Signature...)
		sig[0] ^= 1
		sigs[0].Signature = sig
		rc.Commit.Signatures = sigs
	})
	appHash := forgeAt("commit", func(r any) {
		rc := r.(*coretypes.ResultCommit)
		if rc.Height == bootH+1 {
			rc.AppHash = bytes.Repeat([]byte{0}, 32)
		}
	})
	for _, tc := range []struct {
		name     string
		forge    testchain.Forge
		withGood bool // a second, honest server
		trusted  string
		want     string
	}{
		{name: "validator power", forge: valPower, want: "validator"},
		{name: "block max bytes", forge: maxBytes, want: "ConsensusHash"},
		{name: "commit signature", forge: commitSig, want: "signature"},
		{name: "header app hash", forge: appHash, want: "light block"},
		{name: "wrong trusted hash", trusted: fmt.Sprintf("%X", c.Header(bootH).Hash()), want: "!= trusted"},
		{name: "proposer priority, one server", forge: priority, want: ""},
		{name: "evidence params, one server", forge: evidence, want: ""},
		{name: "proposer priority, two servers", forge: priority, withGood: true, want: "disagree"},
		{name: "evidence params, two servers", forge: evidence, withGood: true, want: "disagree"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := c.ServeRPC(t, tc.forge)
			rpcs := bad.URL
			if tc.withGood {
				rpcs = good.URL + "," + bad.URL
			}
			th := tc.trusted
			if th == "" {
				th = trustedHash(c)
			}
			out, err := run(t, newHome(t, c), "-trusted-hash", th, "-rpc", rpcs, "-dry-run")
			if tc.want == "" { // not authenticated by any header: accepted, as by the stock command
				require.NoError(t, err, out)
				return
			}
			require.ErrorContains(t, err, tc.want)
		})
	}
}
