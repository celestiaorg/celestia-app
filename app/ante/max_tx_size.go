package ante

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxTxSizeDecorator ensures that a tx can not be larger than
// application's configured versioned constant.
type MaxTxSizeDecorator struct{}

func NewMaxTxSizeDecorator() MaxTxSizeDecorator {
	return MaxTxSizeDecorator{}
}

// AnteHandle implements the AnteHandler interface. It ensures that tx size is under application's configured threshold.
func (d MaxTxSizeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// Tx size rule applies to app versions v3 and onwards.
	if ctx.BlockHeader().Version.App < v3.Version {
		return next(ctx, tx, simulate)
	}

	maxTxBytes := appconsts.MaxTxBytes(ctx.BlockHeader().Version.App)
	if len(ctx.TxBytes()) > maxTxBytes {
		return ctx, fmt.Errorf("tx size is larger than the application's configured threshold: %d bytes", maxTxBytes)
	}
	return next(ctx, tx, simulate)
}
