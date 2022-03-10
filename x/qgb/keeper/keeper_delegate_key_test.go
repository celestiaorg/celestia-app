package keeper

import (
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDelegateKeys(t *testing.T) {
	input := CreateTestEnv(t)
	ctx := input.Context
	k := input.QgbKeeper
	var (
		ethAddrs = []string{
			"0x3146D2d6Eed46Afa423969f5dDC3152DfC359b09",
			"0x610277F0208D342C576b991daFdCb36E36515e76",
			"0x835973768750b3ED2D5c3EF5AdcD5eDb44d12aD4",
			"0xb2A7F3E84F8FdcA1da46c810AEa110dd96BAE6bF",
		}

		valAddrs = []string{
			"cosmosvaloper1gghjut3ccd8ay0zduzj64hwre2fxs9ldmqhffj",
			"cosmosvaloper16xyempempp92x9hyzz9wrgf94r6j9h5f2w4n2l",
			"cosmosvaloper1ghekyjucln7y67ntx7cf27m9dpuxxemnsvnaes",
			"cosmosvaloper1tnh2q55v8wyygtt9srz5safamzdengsn9dsd7z",
		}

		orchAddrs = []string{
			"cosmos1er9mgk7x30aspqd2zwn970ywfls36ktdmgyzry",
			"cosmos12ck7y9wrgyk0alnxmnsac75vxr365tw3mn0zsf",
			"cosmos1dz6pu605p5x79dh5pz4dardhuzws6c0qqr0l6e",
			"cosmos1v4s3yfg8rujaz56yt5a3xznqjqgyeff4552l40",
		}
	)

	for i := range ethAddrs {
		// set some addresses
		val, err1 := sdk.ValAddressFromBech32(valAddrs[i])
		orch, err2 := sdk.AccAddressFromBech32(orchAddrs[i])
		require.NoError(t, err1)
		require.NoError(t, err2)
		// set the orchestrator address
		k.SetOrchestratorValidator(ctx, val, orch)
		// set the ethereum address
		ethAddr, err := types.NewEthAddress(ethAddrs[i])
		require.NoError(t, err)
		k.SetEthAddressForValidator(ctx, val, *ethAddr)
	}

	addresses := k.GetDelegateKeys(ctx)
	for i := range addresses {
		res := addresses[i]
		assert.Equal(t, valAddrs[i], res.Validator)
		assert.Equal(t, orchAddrs[i], res.Orchestrator)
		assert.Equal(t, ethAddrs[i], res.EthAddress)
	}
}
