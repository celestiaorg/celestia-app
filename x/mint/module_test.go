package mint_test

import (
	"testing"
)

func TestItCreatesModuleAccountOnInitBlock(t *testing.T) {
	// db := dbm.NewMemDB()
	// encCdc := .MakeTestEncodingConfig()
	// app := simapp.NewSimApp(log.NewNopLogger(), db, nil, true, map[int64]bool{}, simapp.DefaultNodeHome, 5, encCdc, simapp.EmptyAppOptions{})

	// genesisState := simapp.GenesisStateWithSingleValidator(t, app)
	// stateBytes, err := tmjson.Marshal(genesisState)
	// require.NoError(t, err)

	// app.InitChain(
	// 	abcitypes.RequestInitChain{
	// 		AppStateBytes: stateBytes,
	// 		ChainId:       "test-chain-id",
	// 	},
	// )

	// ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	// acc := app.AccountKeeper.GetAccount(ctx, authtypes.NewModuleAddress(types.ModuleName))
	// require.NotNil(t, acc)
}
