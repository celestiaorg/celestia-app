package cmd

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/celestiaorg/celestia-app/v12/app"
	"github.com/celestiaorg/celestia-app/v12/app/encoding"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	"github.com/cometbft/cometbft/types"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_getConsensusParams(t *testing.T) {
	want := types.ConsensusParams{
		Block:     types.BlockParams{MaxBytes: appconsts.DefaultMaxBytes, MaxGas: -1},
		Evidence:  types.EvidenceParams{MaxAgeNumBlocks: 100000, MaxAgeDuration: 172800000000000, MaxBytes: 1048576},
		Validator: types.ValidatorParams{PubKeyTypes: []string{"ed25519"}},
		Version:   types.VersionParams{App: appconsts.Version},
		ABCI:      types.ABCIParams{VoteExtensionsEnableHeight: 0},
	}
	got := *getConsensusParams("")
	assert.Equal(t, want, got)
}

func Test_newPrintInfo(t *testing.T) {
	got, err := json.Marshal(newPrintInfo("moniker", "chain-id", "node-id", "/home"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"moniker":"moniker","chain_id":"chain-id","node_id":"node-id","home":"/home"}`, string(got))
}

func TestInitCortoV12Genesis(t *testing.T) {
	rootCmdMu.Lock()
	defer rootCmdMu.Unlock()
	for _, chain := range []string{appconsts.CortoChainID, appconsts.MochaChainID, appconsts.MainnetChainID, "other"} {
		t.Run(chain, func(t *testing.T) {
			home := t.TempDir()
			root := NewRootCmd()
			root.SetArgs([]string{"init", "validator", "--chain-id", chain, "--home", home})
			require.NoError(t, svrcmd.Execute(root, app.EnvPrefix, home))
			genesis, err := genutiltypes.AppGenesisFromFile(filepath.Join(home, "config", "genesis.json"))
			require.NoError(t, err)
			var modules app.GenesisState
			require.NoError(t, json.Unmarshal(genesis.AppState, &modules))
			var staking stakingtypes.GenesisState
			enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
			require.NoError(t, enc.Codec.UnmarshalJSON(modules[stakingtypes.ModuleName], &staking))
			require.Equal(t, appconsts.GetUnbondingTime(chain, 12), staking.Params.UnbondingTime)
			require.Equal(t, uint64(12), genesis.Consensus.Params.Version.App)
			evidence := genesis.Consensus.Params.Evidence
			if chain == appconsts.CortoChainID {
				require.Equal(t, appconsts.CortoMaxAgeDuration, evidence.MaxAgeDuration)
				require.Equal(t, int64(500), evidence.MaxAgeNumBlocks)
			} else {
				require.Equal(t, getConsensusParams("").Evidence, evidence)
			}
		})
	}
}
