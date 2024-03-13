package ante_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/ante"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	minfeetypes "github.com/celestiaorg/celestia-app/x/minfee"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
	tmdb "github.com/tendermint/tm-db"
)

func TestCheckTxFeeWithGlobalMinGasPrices(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	builder := encCfg.TxConfig.NewTxBuilder()
	err := builder.SetMsgs(banktypes.NewMsgSend(
		testnode.RandomAddress().(sdk.AccAddress),
		testnode.RandomAddress().(sdk.AccAddress),
		sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10))),
	)
	require.NoError(t, err)

	feeAmount := int64(1000)

	testCases := []struct {
		name       string
		fee        sdk.Coins
		gasLimit   uint64
		appVersion uint64
		isCheckTx  bool
		expErr     bool
	}{
		{
			name:       "bad tx; fee below required minimum",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount-1)),
			gasLimit:   uint64(float64(feeAmount) / v2.GlobalMinGasPrice),
			appVersion: uint64(2),
			isCheckTx:  false,
			expErr:     true,
		},
		{
			name:       "good tx; fee equal to required minimum",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:   uint64(float64(feeAmount) / v2.GlobalMinGasPrice),
			appVersion: uint64(2),
			isCheckTx:  false,
			expErr:     false,
		},
		{
			name:       "good tx; fee above required minimum",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount+1)),
			gasLimit:   uint64(float64(feeAmount) / v2.GlobalMinGasPrice),
			appVersion: uint64(2),
			isCheckTx:  false,
			expErr:     false,
		},
		{
			name:       "good tx; with no fee (v1)",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:   uint64(float64(feeAmount) / v2.GlobalMinGasPrice),
			appVersion: uint64(1),
			isCheckTx:  false,
			expErr:     false,
		},
		{
			name:       "good tx; gas limit and fee are maximum values",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, math.MaxInt64)),
			gasLimit:   math.MaxUint64,
			appVersion: uint64(2),
			isCheckTx: false,
			expErr:     false,
		},
		{
			name:       "bad tx; gas limit and fee are 0",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 0)),
			gasLimit:   0,
			appVersion: uint64(2),
			isCheckTx:  false,
			expErr:     false,
		},
		{
			name:       "good tx; minFee = 0.8, rounds up to 1",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:   400,
			appVersion: uint64(2),
			isCheckTx:  false,
			expErr:     false,
		},
		{
			name:       "good tx; fee above node's required minimum",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount+1)),
			gasLimit:   uint64(float64(feeAmount) / appconsts.DefaultGlobalMinGasPrice),
			appVersion: uint64(1),
			isCheckTx:  true,
			expErr:     false,
		},
		{
			name:       "bad tx; fee below node's required minimum",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount-1)),
			gasLimit:   uint64(float64(feeAmount) / appconsts.DefaultGlobalMinGasPrice),
			appVersion: uint64(1),
			isCheckTx:  true,
			expErr:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder.SetGasLimit(tc.gasLimit)
			builder.SetFeeAmount(tc.fee)
			tx := builder.GetTx()

			storeKey := sdk.NewKVStoreKey(paramtypes.StoreKey)
			tStoreKey := storetypes.NewTransientStoreKey(paramtypes.TStoreKey)

			db := tmdb.NewMemDB()
			stateStore := store.NewCommitMultiStore(db)
			stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
			stateStore.MountStoreWithDB(tStoreKey, storetypes.StoreTypeTransient, nil)
			require.NoError(t, stateStore.LoadLatestVersion())

			registry := codectypes.NewInterfaceRegistry()
			cdc := codec.NewProtoCodec(registry)

			ctx := sdk.NewContext(stateStore, tmproto.Header{
				Version: version.Consensus{
					App: tc.appVersion,
				},
			}, tc.isCheckTx, nil)

			paramsSubspace := paramtypes.NewSubspace(cdc,
				testutil.MakeTestCodec(),
				storeKey,
				tStoreKey,
				"GlobalMinGasPrice",
			)

			// Register the parameter in the subspace
			minfeetypes.RegisterMinFeeParamTable(paramsSubspace)

			globalminGasPriceDec, _ := sdk.NewDecFromStr(fmt.Sprintf("%f", v2.GlobalMinGasPrice))
			params := minfeetypes.Params{GlobalMinGasPrice: globalminGasPriceDec}
			paramsSubspace.Set(ctx, minfeetypes.KeyGlobalMinGasPrice, &params.GlobalMinGasPrice)

			_, _, err := ante.CheckTxFeeWithGlobalMinGasPrices(ctx, tx, paramsSubspace)
			if tc.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
