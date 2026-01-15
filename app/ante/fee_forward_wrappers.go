package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

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
