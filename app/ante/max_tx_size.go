package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

// MaxTxSizeDecorator ensures that a tx can not be larger than
// application's configured versioned constant.
type MaxTxSizeDecorator struct{}

func NewMaxTxSizeDecorator() MaxTxSizeDecorator {
	return MaxTxSizeDecorator{}
}

// AnteHandle implements the AnteHandler interface. It ensures that tx size is under application's configured threshold.
func (d MaxTxSizeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	currentTxSize := len(ctx.TxBytes())
	maxTxSize := appconsts.MaxTxSize(ctx.BlockHeader().Version.App)
	if currentTxSize > maxTxSize {
		bytesOverLimit := currentTxSize - maxTxSize
		return ctx, fmt.Errorf("tx size %d bytes is larger than the application's configured threshold of %d bytes. Please reduce the size by %d bytes", currentTxSize, maxTxSize, bytesOverLimit)
	}
	return next(ctx, tx, simulate)
}
