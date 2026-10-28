package ante

import (
	"math/big"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// FuzzAnteFeeArithmetic compares fee rounding and priority against unbounded
// integer arithmetic, including gas limits above MaxInt64 and priority overflow.
// Price is expressed in attoutia/gas (18 decimal places); fees span uint64.
func FuzzAnteFeeArithmetic(f *testing.F) {
	for _, seed := range [][3]uint64{
		{0, 0, 0}, {0, 1, 1}, {1, 1, 1},
		{800, 200_000, 4_000_000_000_000_000},
		{799, 200_000, 4_000_000_000_000_000},
		{1 << 63, 1 << 63, 1_000_000_000_000_000_000},
		{1 << 63, 1, 1}, {^uint64(0), ^uint64(0), ^uint64(0)},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, fee, gas, price uint64) {
		feeInt := math.NewIntFromUint64(fee)
		priceInt := new(big.Int).SetUint64(price)
		minPrice := math.LegacyNewDecFromBigIntWithPrec(priceInt, 18)
		product := new(big.Int).Mul(new(big.Int).SetUint64(gas), priceInt)
		minimum, remainder := new(big.Int), new(big.Int)
		minimum.QuoRem(product, big.NewInt(1_000_000_000_000_000_000), remainder)
		if remainder.Sign() != 0 {
			minimum.Add(minimum, big.NewInt(1))
		}

		err := verifyMinFee(feeInt, gas, minPrice, "fuzz minimum fee")
		require.Equal(t, feeInt.BigInt().Cmp(minimum) >= 0, err == nil)
		// Check both sides of the exact rounded threshold even when the random
		// fee is far from it. The product fits well within the SDK's 256 bits.
		require.NoError(t, verifyMinFee(math.NewIntFromBigInt(minimum), gas, minPrice, "threshold"))
		if minimum.Sign() > 0 {
			below := new(big.Int).Sub(minimum, big.NewInt(1))
			require.Error(t, verifyMinFee(math.NewIntFromBigInt(below), gas, minPrice, "below threshold"))
		}

		var wantPriority int64
		if gas != 0 {
			priority := new(big.Int).Mul(new(big.Int).SetUint64(fee), big.NewInt(1_000_000))
			priority.Quo(priority, new(big.Int).SetUint64(gas))
			if priority.IsInt64() {
				wantPriority = priority.Int64()
			}
		}
		require.Equal(t, wantPriority, getTxPriority(feeInt, gas))
	})
}
