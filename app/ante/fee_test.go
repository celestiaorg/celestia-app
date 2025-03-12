package ante_test

import (
	"fmt"
	"math"
	"testing"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	paramkeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v4/x/minfee"
)

func TestValidateTxFee(t *testing.T) {
	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)

	builder := enc.TxConfig.NewTxBuilder()
	err := builder.SetMsgs(banktypes.NewMsgSend(
		testnode.RandomAddress().(sdk.AccAddress),
		testnode.RandomAddress().(sdk.AccAddress),
		sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10))),
	)
	require.NoError(t, err)

	// Set the validator's fee
	validatorMinGasPrice := 0.8
	validatorMinGasPriceDec, err := sdkmath.LegacyNewDecFromStr(fmt.Sprintf("%f", validatorMinGasPrice))
	require.NoError(t, err)
	validatorMinGasPriceCoin := sdk.NewDecCoinFromDec(appconsts.BondDenom, validatorMinGasPriceDec)

	feeAmount := int64(1000)

	paramsKeeper, stateStore := setUp(t)

	testCases := []struct {
		name      string
		fee       sdk.Coins
		gasLimit  uint64
		isCheckTx bool
		expErr    bool
	}{
		{
			name:      "bad tx; fee below required minimum",
			fee:       sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount-1)),
			gasLimit:  uint64(float64(feeAmount) / appconsts.DefaultNetworkMinGasPrice),
			isCheckTx: false,
			expErr:    true,
		},
		{
			name:      "good tx; fee equal to required minimum",
			fee:       sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:  uint64(float64(feeAmount) / appconsts.DefaultNetworkMinGasPrice),
			isCheckTx: false,
			expErr:    false,
		},
		{
			name:      "good tx; fee above required minimum",
			fee:       sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount+1)),
			gasLimit:  uint64(float64(feeAmount) / appconsts.DefaultNetworkMinGasPrice),
			isCheckTx: false,
			expErr:    false,
		},
		{
			name:      "good tx; gas limit and fee are maximum values",
			fee:       sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, math.MaxInt64)),
			gasLimit:  math.MaxUint64,
			isCheckTx: false,
			expErr:    false,
		},
		{
			name:      "bad tx; gas limit and fee are 0",
			fee:       sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 0)),
			gasLimit:  0,
			isCheckTx: false,
			expErr:    false,
		},
		{
			name:      "good tx; minFee = 0.8, rounds up to 1",
			fee:       sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:  400,
			isCheckTx: false,
			expErr:    false,
		},
		{
			name:      "good tx; fee above node's required minimum",
			fee:       sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount+1)),
			gasLimit:  uint64(float64(feeAmount) / validatorMinGasPrice),
			isCheckTx: true,
			expErr:    false,
		},
		{
			name:      "bad tx; fee below node's required minimum",
			fee:       sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount-1)),
			gasLimit:  uint64(float64(feeAmount) / validatorMinGasPrice),
			isCheckTx: true,
			expErr:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder.SetGasLimit(tc.gasLimit)
			builder.SetFeeAmount(tc.fee)
			tx := builder.GetTx()

			ctx := sdk.NewContext(stateStore, tmproto.Header{}, tc.isCheckTx, log.NewNopLogger())
			ctx = ctx.WithMinGasPrices(sdk.DecCoins{validatorMinGasPriceCoin})

			networkMinGasPriceDec, err := sdkmath.LegacyNewDecFromStr(fmt.Sprintf("%f", appconsts.DefaultNetworkMinGasPrice))
			require.NoError(t, err)

			subspace, _ := paramsKeeper.GetSubspace(minfee.ModuleName)
			subspace = minfee.RegisterMinFeeParamTable(subspace)
			subspace.Set(ctx, minfee.KeyNetworkMinGasPrice, networkMinGasPriceDec)

			_, _, err = ante.ValidateTxFee(ctx, tx, paramsKeeper)
			if tc.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func setUp(t *testing.T) (paramkeeper.Keeper, storetypes.CommitMultiStore) {
	storeKey := storetypes.NewKVStoreKey(paramtypes.StoreKey)
	tStoreKey := storetypes.NewTransientStoreKey(paramtypes.TStoreKey)

	// Create the state store
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(tStoreKey, storetypes.StoreTypeTransient, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()

	// Create a params keeper and set the network min gas price.
	paramsKeeper := paramkeeper.NewKeeper(codec.NewProtoCodec(registry), codec.NewLegacyAmino(), storeKey, tStoreKey)
	paramsKeeper.Subspace(minfee.ModuleName)
	return paramsKeeper, stateStore
}
