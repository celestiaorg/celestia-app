package keeper

import (
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Export unexported functions for testing.

func IsSupportedDenom(denom string) bool {
	return isSupportedDenom(denom)
}

func FilterSupportedDenoms(coins sdk.Coins) sdk.Coins {
	return filterSupportedDenoms(coins)
}

func CalculateExcessIGPFee(before, after, quotedFee, forwardedBalance sdk.Coin) math.Int {
	return calculateExcessIGPFee(before, after, quotedFee, forwardedBalance)
}
