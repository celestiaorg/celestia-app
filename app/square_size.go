package app

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxEffectiveSquareSize returns the max effective square size.
func (app *App) MaxEffectiveSquareSize(ctx sdk.Context) int {
	// TODO: fix hack that forces the max square size for the first height to
	// 64. This is due to our fork of the sdk not initializing state before
	// BeginBlock of the first block. This is remedied in versions of the sdk
	// and comet that have full support of PreparePropsoal, although
	// celestia-app does not currently use those. see this PR for more details
	// https://github.com/cosmos/cosmos-sdk/pull/14505
	if ctx.BlockHeader().Height <= 1 {
		return int(appconsts.DefaultGovMaxSquareSize)
	}
	fmt.Printf("foo")

	govMax := int(app.BlobKeeper.GovMaxSquareSize(ctx))
	hardMax := appconsts.SquareSizeUpperBound(app.AppVersion())
	return min(govMax, hardMax)
}
