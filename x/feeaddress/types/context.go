package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// FeeForwardContextKey indicates a transaction is a protocol-injected fee forward transaction.
// Set by EarlyFeeForwardDetector BEFORE ValidateBasic runs, enabling SkipForFeeForwardDecorator
// to skip signature-related decorators for this unsigned transaction.
type FeeForwardContextKey struct{}

// FeeForwardAmountContextKey stores the fee amount deducted from the fee address.
// Set by FeeForwardDecorator AFTER the bank transfer completes. The keeper's
// ForwardFees message handler reads this to emit EventFeeForwarded.
type FeeForwardAmountContextKey struct{}

// IsFeeForwardTx returns true if the context indicates this is a fee forward transaction.
func IsFeeForwardTx(ctx sdk.Context) bool {
	val := ctx.Value(FeeForwardContextKey{})
	if val == nil {
		return false
	}
	isFeeForward, ok := val.(bool)
	return ok && isFeeForward
}

// GetFeeForwardAmount returns the fee amount that was forwarded, if available in context.
func GetFeeForwardAmount(ctx sdk.Context) (sdk.Coins, bool) {
	val := ctx.Value(FeeForwardAmountContextKey{})
	if val == nil {
		return nil, false
	}
	fee, ok := val.(sdk.Coins)
	return fee, ok
}
