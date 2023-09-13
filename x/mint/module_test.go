package mint_test

import (
	"testing"

	dbm "github.com/cometbft/cometbft-db"
	abcitypes "github.com/cometbft/cometbft/abci/types"
	tmjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/celestiaorg/celestia-app/x/mint/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func TestItCreatesModuleAccountOnInitBlock(t *testing.T) {
	db := dbm.NewMemDB()
	encCdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	app := app.New(log.NewNopLogger(), db, nil, true, map[int64]bool{}, app.DefaultNodeHome, 5, encCdc, nil)

	genesisState, _, _ := util.GenesisStateWithSingleValidator(app)
	stateBytes, err := tmjson.Marshal(genesisState)
	require.NoError(t, err)

	app.InitChain(
		abcitypes.RequestInitChain{
			AppStateBytes: stateBytes,
			ChainId:       "test-chain-id",
		},
	)

	cfg := testnode.DefaultConfig()
	cctx, _, _ := testnode.NewNetwork(t, cfg)

	// Check that the module account was created
	accMsgSvr := authtypes.NewQueryClient(cctx.GRPCClient)

	resp, err := accMsgSvr.Account(cctx.GoContext(), &authtypes.QueryAccountRequest{
		Address: authtypes.NewModuleAddress(types.ModuleName).String(),
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Account)
}
