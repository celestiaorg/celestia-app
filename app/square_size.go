package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

// MaxEffectiveSquareSize returns the max effective square size.
func (app *App) MaxEffectiveSquareSize(ctx sdk.Context) int {
	govMax := app.BlobKeeper.GetParams(ctx).GovMaxSquareSize
	hardMax := appconsts.GetSquareSizeUpperBound(ctx.ChainID())
	return min(int(govMax), hardMax)
}
