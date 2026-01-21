package ante

import (
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.AnteDecorator = EarlyFeeForwardDetector{}
	_ sdk.AnteDecorator = SkipForFeeForwardDecorator{}
)

// EarlyFeeForwardDetector detects MsgForwardFees transactions early in the ante chain
// and sets a context flag. This flag is used by SkipForFeeForwardDecorator to skip
// signature-related decorators since fee forward transactions have no signers.
type EarlyFeeForwardDetector struct{}

// NewEarlyFeeForwardDetector creates a new EarlyFeeForwardDetector.
func NewEarlyFeeForwardDetector() EarlyFeeForwardDetector {
	return EarlyFeeForwardDetector{}
}

// AnteHandle implements sdk.AnteDecorator.
func (d EarlyFeeForwardDetector) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if feeaddresstypes.IsFeeForwardMsg(tx) != nil {
		// Set the context flag so signature-related decorators can be skipped
		ctx = ctx.WithValue(feeaddresstypes.FeeForwardContextKey{}, true)
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
	if feeaddresstypes.IsFeeForwardTx(ctx) {
		return next(ctx, tx, simulate)
	}
	return d.inner.AnteHandle(ctx, tx, simulate, next)
}

// Re-export context key types from feeaddresstypes for use in tests and other packages
// that expect these to be in the ante package.
type (
	// FeeForwardContextKey is re-exported from feeaddresstypes for backward compatibility.
	FeeForwardContextKey = feeaddresstypes.FeeForwardContextKey
	// FeeForwardAmountContextKey is re-exported from feeaddresstypes for backward compatibility.
	FeeForwardAmountContextKey = feeaddresstypes.FeeForwardAmountContextKey
)

// IsFeeForwardTx is re-exported from feeaddresstypes for backward compatibility.
var IsFeeForwardTx = feeaddresstypes.IsFeeForwardTx

// GetFeeForwardAmount is re-exported from feeaddresstypes for backward compatibility.
var GetFeeForwardAmount = feeaddresstypes.GetFeeForwardAmount
