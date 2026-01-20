package ante

import (
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EarlyFeeForwardDetector detects MsgForwardFees transactions early in the ante chain
// and sets the context flag so that subsequent decorators (like ValidateBasic) can be skipped.
// This MUST be placed before ValidateBasicDecorator in the ante chain.
type EarlyFeeForwardDetector struct{}

// NewEarlyFeeForwardDetector creates a new EarlyFeeForwardDetector.
func NewEarlyFeeForwardDetector() EarlyFeeForwardDetector {
	return EarlyFeeForwardDetector{}
}

// AnteHandle implements sdk.AnteDecorator.
func (d EarlyFeeForwardDetector) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if feeaddresstypes.IsFeeForwardMsg(tx) != nil {
		// Set the context flag early so ValidateBasic and other decorators can be skipped
		ctx = ctx.WithValue(FeeForwardContextKey{}, true)
	}
	return next(ctx, tx, simulate)
}

// SkipForFeeForwardDecorator wraps an ante decorator and skips it for fee forward transactions.
type SkipForFeeForwardDecorator struct {
	inner sdk.AnteDecorator
}

// NewSkipForFeeForwardDecorator creates a wrapper that skips the inner decorator for fee forward txs.
func NewSkipForFeeForwardDecorator(inner sdk.AnteDecorator) SkipForFeeForwardDecorator {
	return SkipForFeeForwardDecorator{inner: inner}
}

// AnteHandle implements sdk.AnteDecorator.
func (d SkipForFeeForwardDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if IsFeeForwardTx(ctx) {
		return next(ctx, tx, simulate)
	}
	return d.inner.AnteHandle(ctx, tx, simulate, next)
}
