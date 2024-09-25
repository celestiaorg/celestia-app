package ante

import (
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// MaxTxSizeDecorator ensures that a tx can not be larger than
// application's configured versioned constant.
type MaxTxSizeDecorator struct{}

func NewMaxTxSizeDecorator() MaxTxSizeDecorator {
	return MaxTxSizeDecorator{}
}

// AnteHandle implements the AnteHandler interface. It ensures that tx size is under application's configured threshold.
func (d MaxTxSizeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// This is a tx validity check, therefore it only applies to CheckTx.
	// Tx size rule applies to app versions v3 and onwards.
	if ctx.IsCheckTx() && ctx.BlockHeader().Version.App >= v3.Version {
		if len(ctx.TxBytes()) >= appconsts.TxMaxBytes(ctx.BlockHeader().Version.App) {
			return ctx, sdkerrors.ErrTxTooLarge
		}
	}
	return next(ctx, tx, simulate)
}
