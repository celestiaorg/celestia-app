package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v12/app"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v12/pkg/hardfork/genesis"
	"github.com/celestiaorg/celestia-app/v12/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	cryptoenc "github.com/cometbft/cometbft/crypto/encoding"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/codec"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

func TestGenesisFromExport(t *testing.T) {
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	exported, err := testApp.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	// An export at E = 1, wrapped as the SDK export command does.
	export := &genutiltypes.AppGenesis{
		AppName:       "celestia-appd",
		AppVersion:    "v0.0.0-test",
		GenesisTime:   util.GenesisTime,
		ChainID:       util.ChainID,
		InitialHeight: exported.Height,
		AppState:      exported.AppState,
		Consensus:     genutiltypes.NewConsensusGenesis(exported.ConsensusParams, exported.Validators),
	}
	// write saves g in a new directory and returns the from-export arguments for it.
	write := func(t *testing.T, g *genutiltypes.AppGenesis) (string, []string) {
		bz, err := json.Marshal(g)
		require.NoError(t, err)
		dir := t.TempDir()
		path := filepath.Join(dir, "export.json")
		require.NoError(t, os.WriteFile(path, bz, 0o600))
		sum := sha256.Sum256(bz)
		return dir, []string{
			path,
			"--export-sha256", hex.EncodeToString(sum[:]),
			"--export-height", "1",
			"--export-block-time", util.GenesisTime.Format(time.RFC3339Nano),
			"--genesis-time", util.GenesisTime.Add(time.Hour).Format(time.RFC3339Nano),
			"--initial-height", "12",
			"--output", filepath.Join(dir, "genesis.json"),
		}
	}
	run := func(args []string) (string, error) {
		cmd := genesisFromExportCmd(testApp)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}

	t.Run("success is deterministic", func(t *testing.T) {
		var files, reports [2][]byte
		for i := range files {
			dir, args := write(t, export)
			out, err := run(args)
			require.NoError(t, err)
			files[i], err = os.ReadFile(filepath.Join(dir, "genesis.json"))
			require.NoError(t, err)
			sum := sha256.Sum256(files[i])
			reportPath := filepath.Join(dir, "genesis.json.report.json")
			require.Equal(t, "genesis_sha256 "+hex.EncodeToString(sum[:])+"\nreport "+reportPath+"\n", out)

			reports[i], err = os.ReadFile(reportPath)
			require.NoError(t, err)
			rep := readReport(t, reportPath)
			require.Equal(t, hex.EncodeToString(sum[:]), rep.GenesisSHA256)
			require.Contains(t, rep.Status, "runbook")
			require.Equal(t, genesis.Change{Export: "2", Candidate: "12"}, rep.InitialHeight)
			require.Empty(t, rep.ChainIDWarning)
			require.NotNil(t, rep.Checks)
			require.False(t, rep.Checks.Supply.Empty())
			v := export.Consensus.Validators[0]
			require.Len(t, export.Consensus.Validators, 1)
			require.Equal(t, []genesis.Validator{{Address: v.PubKey.Address().String(), Power: v.Power}}, rep.Validators)
			require.Equal(t, v.Power, rep.TotalPower)
		}
		require.Equal(t, files[0], files[1])
		require.Equal(t, reports[0], reports[1])
		requireImports(t, testApp.AppCodec(), files[0])
	})

	t.Run("root command leaves home untouched", func(t *testing.T) {
		home := t.TempDir()
		dir, args := write(t, export)
		root := NewRootCmd()
		root.SetArgs(append(append([]string{"genesis", "from-export"}, args...), "--home", home))
		root.SetOut(io.Discard)
		// As main does; it adds --home. The root pre-run would create config files in home.
		require.NoError(t, svrcmd.Execute(root, app.EnvPrefix, home))
		entries, err := os.ReadDir(home)
		require.NoError(t, err)
		require.Empty(t, entries)
		require.FileExists(t, filepath.Join(dir, "genesis.json"))
	})

	t.Run("new chain-id warns about IBC", func(t *testing.T) {
		dir, args := write(t, export)
		_, err := run(append(args, "--chain-id", "test-app-2", "--accept-mint-excess", "0"))
		require.NoError(t, err)
		rep := readReport(t, filepath.Join(dir, "genesis.json.report.json"))
		require.Equal(t, genesis.Change{Export: util.ChainID, Candidate: "test-app-2"}, rep.ChainID)
		require.Contains(t, rep.ChainIDWarning, "MsgRecoverClient")
		require.Equal(t, "0.000000000000000000", rep.AcceptMintExcess)
	})

	refusals := []struct {
		name string
		edit func(t *testing.T) (string, []string)
		want string
	}{
		{"wrong export sha", func(t *testing.T) (string, []string) {
			dir, args := write(t, export)
			args[2] = hex.EncodeToString(make([]byte, sha256.Size))
			return dir, args
		}, "export-sha256"},
		{"missing genesis time", func(t *testing.T) (string, []string) {
			dir, args := write(t, export)
			i := slices.Index(args, "--genesis-time")
			return dir, slices.Delete(args, i, i+2)
		}, `required flag(s) "genesis-time" not set`},
		{"existing output", func(t *testing.T) (string, []string) {
			dir, args := write(t, export)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "genesis.json"), []byte("kept"), 0o600))
			return dir, args
		}, "absent"},
		{"validator set differs from staking", func(t *testing.T) (string, []string) {
			return write(t, withValidator(export, func(v *cmttypes.GenesisValidator) { v.Power++ }))
		}, "as in staking"},
		{"validator address filled by the SDK", func(t *testing.T) (string, []string) {
			return write(t, withValidator(export, func(v *cmttypes.GenesisValidator) { v.Address = nil }))
		}, "AppGenesis.ValidateAndComplete"},
		{"module ValidateGenesis fails", func(t *testing.T) (string, []string) {
			return write(t, withAppState(t, export, `"signed_blocks_window":"32500"`, `"signed_blocks_window":"0"`))
		}, `validate: "slashing"`},
		// Bank ValidateGenesis rejects it too; the CheckState error shows that CheckState runs first.
		{"supply differs from balances", func(t *testing.T) (string, []string) {
			return write(t, withAppState(t, export, `"amount":"100000001000000"}]`, `"amount":"100000001000001"}]`))
		}, `conservation: "bank": "supply[utia]"`},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			dir, args := tc.edit(t)
			_, err := run(args)
			require.ErrorContains(t, err, tc.want)
			_, err = os.Stat(filepath.Join(dir, "genesis.json.report.json"))
			require.ErrorIs(t, err, os.ErrNotExist)
			bz, err := os.ReadFile(filepath.Join(dir, "genesis.json"))
			if tc.name == "existing output" {
				require.Equal(t, "kept", string(bz))
			} else {
				require.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}

// withValidator returns a copy of g with edit applied to its first consensus validator.
func withValidator(g *genutiltypes.AppGenesis, edit func(*cmttypes.GenesisValidator)) *genutiltypes.AppGenesis {
	c := *g
	c.Consensus = &genutiltypes.ConsensusGenesis{Params: g.Consensus.Params, Validators: slices.Clone(g.Consensus.Validators)}
	edit(&c.Consensus.Validators[0])
	return &c
}

func readReport(t *testing.T, path string) genesis.Report {
	t.Helper()
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	var rep genesis.Report
	require.NoError(t, json.Unmarshal(bz, &rep))
	return rep
}

// requireImports runs InitChain on a fresh app with the genesis file as core sends it, then one block.
func requireImports(t *testing.T, cdc codec.JSONCodec, file []byte) {
	t.Helper()
	var g genutiltypes.AppGenesis
	require.NoError(t, json.Unmarshal(file, &g))
	var appState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(g.AppState, &appState))
	supply := banktypes.GetGenesisStateFromAppState(cdc, appState).Supply
	vals := make([]abci.ValidatorUpdate, len(g.Consensus.Validators))
	set := make([]*cmttypes.Validator, len(g.Consensus.Validators))
	for i, v := range g.Consensus.Validators {
		pk, err := cryptoenc.PubKeyToProto(v.PubKey)
		require.NoError(t, err)
		vals[i] = abci.ValidatorUpdate{PubKey: pk, Power: v.Power}
		set[i] = cmttypes.NewValidator(v.PubKey, v.Power)
	}
	params := g.Consensus.Params.ToProto()

	fresh := util.NewTestApp()
	_, err := fresh.Info(&abci.RequestInfo{})
	require.NoError(t, err)
	// baseapp fails InitChain unless the staking validators equal vals.
	res, err := fresh.InitChain(&abci.RequestInitChain{
		Time:            g.GenesisTime,
		ChainId:         g.ChainID,
		ConsensusParams: &params,
		Validators:      vals,
		AppStateBytes:   g.AppState,
		InitialHeight:   g.InitialHeight,
	})
	require.NoError(t, err)
	require.ElementsMatch(t, vals, res.Validators)

	block, err := fresh.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:               g.GenesisTime,
		Height:             g.InitialHeight,
		Hash:               fresh.LastCommitID().Hash,
		NextValidatorsHash: cmttypes.NewValidatorSet(set).Hash(),
	})
	require.NoError(t, err)
	require.Empty(t, block.ValidatorUpdates)
	_, err = fresh.Commit()
	require.NoError(t, err)
	require.Equal(t, g.InitialHeight, fresh.LastBlockHeight())

	// Import clears the previous block time, so the first block mints nothing.
	ctx := fresh.NewUncachedContext(false, cmtproto.Header{})
	got, _, err := fresh.BankKeeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{Limit: query.PaginationMaxLimit})
	require.NoError(t, err)
	require.Equal(t, supply, got)
	info, err := fresh.Info(&abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, appconsts.Version, info.AppVersion)
}

// withAppState returns a copy of g with the single occurrence of old in the compact app_state replaced.
func withAppState(t *testing.T, g *genutiltypes.AppGenesis, old, repl string) *genutiltypes.AppGenesis {
	var b bytes.Buffer
	require.NoError(t, json.Compact(&b, g.AppState))
	require.Equal(t, 1, bytes.Count(b.Bytes(), []byte(old)))
	c := *g
	c.AppState = bytes.Replace(b.Bytes(), []byte(old), []byte(repl), 1)
	return &c
}
