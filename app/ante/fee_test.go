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
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/ante"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	minfeekeeper "github.com/celestiaorg/celestia-app/v6/x/minfee/keeper"
	minfeetypes "github.com/celestiaorg/celestia-app/v6/x/minfee/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	paramkeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"
)

func TestValidateTxFee(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	builder := enc.TxConfig.NewTxBuilder()
	err := builder.SetMsgs(banktypes.NewMsgSend(
		testnode.RandomAddress().(sdk.AccAddress),
		testnode.RandomAddress().(sdk.AccAddress),
		sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10))),
	)
	require.NoError(t, err)

	// Set the validator's fee
	validatorMinGasPrice := 0.8
	validatorMinGasPriceCoin := fmt.Sprintf("%f%s", validatorMinGasPrice, appconsts.BondDenom)

	feeAmount := int64(1000)

	paramsKeeper, minFeeKeeper, stateStore := setUp(t)

	testCases := []struct {
		name        string
		fee         sdk.Coins
		gasLimit    uint64
		isCheckTx   bool
		expErr      bool
		minGasPrice string
	}{
		{
			name:        "bad tx; fee below required minimum",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount-1)),
			gasLimit:    uint64(float64(feeAmount) / appconsts.DefaultNetworkMinGasPrice),
			isCheckTx:   false,
			expErr:      true,
			minGasPrice: validatorMinGasPriceCoin,
		},
		{
			name:        "good tx; fee equal to required minimum",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:    uint64(float64(feeAmount) / appconsts.DefaultNetworkMinGasPrice),
			isCheckTx:   false,
			expErr:      false,
			minGasPrice: validatorMinGasPriceCoin,
		},
		{
			name:        "good tx; fee above required minimum",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount+1)),
			gasLimit:    uint64(float64(feeAmount) / appconsts.DefaultNetworkMinGasPrice),
			isCheckTx:   false,
			expErr:      false,
			minGasPrice: validatorMinGasPriceCoin,
		},
		{
			name:        "good tx; gas limit and fee are maximum values",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, math.MaxInt64)),
			gasLimit:    math.MaxUint64,
			isCheckTx:   false,
			expErr:      false,
			minGasPrice: validatorMinGasPriceCoin,
		},
		{
			name:        "bad tx; gas limit and fee are 0",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 0)),
			gasLimit:    0,
			isCheckTx:   false,
			expErr:      false,
			minGasPrice: validatorMinGasPriceCoin,
		},
		{
			name:        "good tx; minFee = 0.8, rounds up to 1",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:    400,
			isCheckTx:   false,
			expErr:      false,
			minGasPrice: validatorMinGasPriceCoin,
		},
		{
			name:        "good tx; fee above node's required minimum",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount+1)),
			gasLimit:    uint64(float64(feeAmount) / validatorMinGasPrice),
			isCheckTx:   true,
			expErr:      false,
			minGasPrice: validatorMinGasPriceCoin,
		},
		{
			name:        "bad tx; fee below node's required minimum",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount-1)),
			gasLimit:    uint64(float64(feeAmount) / validatorMinGasPrice),
			isCheckTx:   true,
			expErr:      true,
			minGasPrice: validatorMinGasPriceCoin,
		},
		{
			name:        "min gas price is empty",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:    uint64(float64(feeAmount) / appconsts.DefaultMinGasPrice),
			isCheckTx:   true,
			expErr:      false,
			minGasPrice: "", // should use the default min gas price
		},
		{
			name:        "min gas price is empty",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount-1)),
			gasLimit:    uint64(float64(feeAmount) / appconsts.DefaultMinGasPrice),
			isCheckTx:   true,
			expErr:      true,
			minGasPrice: "", // should use the default min gas price
		},
		{
			name:        "min gas price is 0utia",
			fee:         sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:    uint64(float64(feeAmount) / appconsts.DefaultMinGasPrice),
			isCheckTx:   true,
			expErr:      false,
			minGasPrice: "0utia",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder.SetGasLimit(tc.gasLimit)
			builder.SetFeeAmount(tc.fee)
			tx := builder.GetTx()

			ctx := sdk.NewContext(stateStore, tmproto.Header{}, tc.isCheckTx, log.NewNopLogger())
			minPrice, err := sdk.ParseDecCoins(tc.minGasPrice)
			require.NoError(t, err)
			ctx = ctx.WithMinGasPrices(minPrice)

			networkMinGasPriceDec, err := sdkmath.LegacyNewDecFromStr(fmt.Sprintf("%f", appconsts.DefaultNetworkMinGasPrice))
			require.NoError(t, err)

			subspace, _ := paramsKeeper.GetSubspace(minfeetypes.ModuleName)
			subspace = minfeetypes.RegisterMinFeeParamTable(subspace)
			subspace.Set(ctx, minfeetypes.KeyNetworkMinGasPrice, networkMinGasPriceDec)

			minFeeKeeper.SetParams(ctx, minfeetypes.Params{
				NetworkMinGasPrice: networkMinGasPriceDec,
			})

			_, _, err = ante.ValidateTxFee(ctx, tx, minFeeKeeper)
			if tc.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestParseMinGasPrice(t *testing.T) {
	emptyCoins, err := sdk.ParseDecCoins("")
	require.NoError(t, err)
	require.Equal(t, emptyCoins.String(), "")
	require.Len(t, emptyCoins, 0)

	oneCoin, err := sdk.ParseDecCoins("0utia")
	require.NoError(t, err)
	require.Zero(t, oneCoin.AmountOf(appconsts.BondDenom).BigInt().Int64())
}

func setUp(t *testing.T) (paramkeeper.Keeper, *minfeekeeper.Keeper, storetypes.CommitMultiStore) {
	storeKey := storetypes.NewKVStoreKey(paramtypes.StoreKey)
	mfStoreKey := storetypes.NewKVStoreKey(minfeetypes.StoreKey)
	tStoreKey := storetypes.NewTransientStoreKey(paramtypes.TStoreKey)

	// Create the state store
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(mfStoreKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(tStoreKey, storetypes.StoreTypeTransient, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()

	// Create a params keeper and set the network min gas price.
	paramsKeeper := paramkeeper.NewKeeper(codec.NewProtoCodec(registry), codec.NewLegacyAmino(), storeKey, tStoreKey)
	subspace := paramsKeeper.Subspace(minfeetypes.ModuleName)

	mfk := minfeekeeper.NewKeeper(encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec, mfStoreKey, paramsKeeper, subspace, "")
	return paramsKeeper, mfk, stateStore
}
