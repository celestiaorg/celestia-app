package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

// MaxEffectiveSquareSize returns the max effective square size.
func (app *App) MaxEffectiveSquareSize(ctx sdk.Context) int {
	govMax := int(app.BlobKeeper.GetParams(ctx).GovMaxSquareSize)
	hardMax := appconsts.DefaultSquareSizeUpperBound
	return min(govMax, hardMax)
}
