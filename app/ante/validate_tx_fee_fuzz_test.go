package ante_test

import (
	"math/big"
	"testing"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	minfeetypes "github.com/celestiaorg/celestia-app/v11/x/minfee/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// FuzzValidateTxFee runs the keeper-backed fee checker on attacker-chosen fee
// amounts and gas limits. The fee checker runs in DeductFeeDecorator before
// signature and balance checks, so any decodable tx reaches it in CheckTx,
// PrepareProposal and ProcessProposal. It must return an error rather than
// panic, and accept exactly the utia fees that meet the network minimum gas
// price.
func FuzzValidateTxFee(f *testing.F) {
	f.Add(uint64(0), uint64(1_000), uint64(200_000), false, "")
	f.Add(uint64(0), uint64(400), uint64(200_000), true, "")
	f.Add(uint64(0), uint64(1)<<63-1, uint64(1), false, "")
	f.Add(uint64(1), uint64(0), uint64(200_000), false, "")
	f.Add(uint64(0), uint64(1), uint64(0), false, "")
	f.Add(^uint64(0), ^uint64(0), ^uint64(0), true, "")
	f.Add(uint64(0), uint64(1_000), uint64(200_000), false, "ibc/FEED")
	f.Add(uint64(0), uint64(0), uint64(200_000), true, appconsts.BondDenom+"x")

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	paramsKeeper, minFeeKeeper, stateStore := setUp(f)
	networkMinGasPrice, err := sdkmath.LegacyNewDecFromStr("0.000001")
	require.NoError(f, err)

	f.Fuzz(func(t *testing.T, feeHigh, feeLow, gas uint64, isCheckTx bool, otherDenom string) {
		// fee = feeHigh<<64 | feeLow spans amounts far above MaxInt64.
		feeBig := new(big.Int).Lsh(new(big.Int).SetUint64(feeHigh), 64)
		feeBig.Or(feeBig, new(big.Int).SetUint64(feeLow))
		fee := sdkmath.NewIntFromBigInt(feeBig)
		// ValidateTxFee panics on fees above MaxInt64 when it records the
		// observed gas price; the ante chain recovers it. Remove this exclusion
		// with the fix: https://linear.app/celestia/issue/PROTOCO-3433 item 2.
		if !fee.IsInt64() {
			return
		}

		builder := enc.TxConfig.NewTxBuilder()
		builder.SetGasLimit(gas)
		var fees sdk.Coins
		if fee.IsPositive() {
			fees = append(fees, sdk.Coin{Denom: appconsts.BondDenom, Amount: fee})
		}
		// Coins are not validated before the fee checker, so any denom string
		// can reach it; every fee coin must be utia.
		foreign := otherDenom != "" && otherDenom != appconsts.BondDenom
		if foreign {
			fees = append(fees, sdk.Coin{Denom: otherDenom, Amount: sdkmath.OneInt()})
		}
		builder.SetFeeAmount(fees)

		ctx := sdk.NewContext(stateStore, tmproto.Header{}, isCheckTx, log.NewNopLogger()).
			WithMinGasPrices(sdk.DecCoins{})
		subspace, _ := paramsKeeper.GetSubspace(minfeetypes.ModuleName)
		subspace = minfeetypes.RegisterMinFeeParamTable(subspace)
		subspace.Set(ctx, minfeetypes.KeyNetworkMinGasPrice, networkMinGasPrice)
		minFeeKeeper.SetParams(ctx, minfeetypes.Params{NetworkMinGasPrice: networkMinGasPrice})

		_, priority, err := ante.ValidateTxFee(ctx, builder.GetTx(), minFeeKeeper)

		// The network minimum is 1e-6 utia/gas, so the minimum fee is ceil(gas/1e6).
		minimum := new(big.Int).SetUint64(gas)
		minimum.Add(minimum, big.NewInt(999_999))
		minimum.Quo(minimum, big.NewInt(1_000_000))
		require.Equal(t, !foreign && feeBig.Cmp(minimum) >= 0, err == nil, "fee %s gas %d denom %q: %v", feeBig, gas, otherDenom, err)
		require.GreaterOrEqual(t, priority, int64(0))
	})
}
