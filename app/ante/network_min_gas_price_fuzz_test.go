package ante_test

import (
	"math/big"
	"sync"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	minfeetypes "github.com/celestiaorg/celestia-app/v11/x/minfee/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

var (
	minGasPriceAppOnce sync.Once
	minGasPriceApp     *app.App
)

// maxGovMinGasPriceBits bounds the fuzzed network minimum gas price at about
// 3.4e20 utia/gas, far above any price governance would set. Larger prices
// overflow LegacyDec when multiplied by a uint64 gas limit.
const maxGovMinGasPriceBits = 128

// FuzzNetworkMinGasPrice sets the network minimum gas price through
// MsgUpdateMinfeeParams, which accepts any value because Params.Validate
// checks nothing, and then validates fees against it in DeliverTx mode. The
// message goes through the wire encoding, as a governance proposal does, so
// an omitted price arrives as the decoder produces it. ValidateTxFee must never
// panic, and must accept a fee exactly when fee >= ceil(price * gas), computed
// independently with big.Int; a negative price requires nothing.
func FuzzNetworkMinGasPrice(f *testing.F) {
	f.Add(false, []byte{}, uint64(0), uint64(0))
	f.Add(false, big.NewInt(4_000_000_000_000_000).Bytes(), uint64(200_000), uint64(800)) // 0.004 utia/gas
	f.Add(false, big.NewInt(4_000_000_000_000_000).Bytes(), uint64(200_000), uint64(799)) // one short
	f.Add(true, big.NewInt(1_000_000_000_000_000_000).Bytes(), uint64(1), uint64(0))      // negative price
	f.Add(false, []byte{1}, ^uint64(0), uint64(0))                                        // 1e-18 utia/gas, max gas
	f.Add(false, new(big.Int).Lsh(big.NewInt(1), 127).Bytes(), ^uint64(0), ^uint64(0))    // huge price
	f.Add(false, big.NewInt(1_000_000_000_000_000_001).Bytes(), uint64(3), uint64(4))     // rounding up

	f.Fuzz(func(t *testing.T, negative bool, magnitude []byte, gas, fee uint64) {
		minGasPriceAppOnce.Do(func() {
			minGasPriceApp, _, _ = testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
		})
		testApp := minGasPriceApp
		mag := new(big.Int).SetBytes(magnitude)
		if mag.BitLen() > maxGovMinGasPriceBits {
			return
		}
		price := new(big.Int).Set(mag)
		if negative {
			price.Neg(price)
		}

		// DeliverTx mode: only the network minimum applies. The context is a
		// fresh branch, so earlier inputs' parameter updates do not leak in.
		ctx := testApp.NewContext(false)
		msg := &minfeetypes.MsgUpdateMinfeeParams{
			Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
			Params:    minfeetypes.Params{NetworkMinGasPrice: math.LegacyNewDecFromBigIntWithPrec(price, math.LegacyPrecision)},
		}
		wire, err := msg.Marshal()
		require.NoError(t, err)
		var decoded minfeetypes.MsgUpdateMinfeeParams
		require.NoError(t, decoded.Unmarshal(wire))
		_, err = testApp.MinFeeKeeper.UpdateMinfeeParams(ctx, &decoded)
		require.NoError(t, err, "the governance authority's update was rejected")

		builder := testApp.GetTxConfig().NewTxBuilder()
		builder.SetGasLimit(gas)
		builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewIntFromUint64(fee))))
		_, priority, err := ante.ValidateTxFee(ctx, builder.GetTx(), testApp.MinFeeKeeper)

		// required = ceil(price * gas / 1e18), using the stored price.
		stored := testApp.MinFeeKeeper.GetParams(ctx).NetworkMinGasPrice.BigInt()
		product := new(big.Int).Mul(stored, new(big.Int).SetUint64(gas))
		required, rem := new(big.Int).QuoRem(product, big.NewInt(1_000_000_000_000_000_000), new(big.Int))
		if rem.Sign() > 0 {
			required.Add(required, big.NewInt(1))
		}
		wantAccept := new(big.Int).SetUint64(fee).Cmp(required) >= 0
		require.Equal(t, wantAccept, err == nil, "price %s, gas %d, fee %d: %v", stored, gas, fee, err)
		require.GreaterOrEqual(t, priority, int64(0))
		if negative || mag.Sign() == 0 {
			require.NoError(t, err, "a non-positive network price must not require a fee")
		}
	})
}
