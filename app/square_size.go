package app

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GovMaxSquareSize returns the maximum square size that can be used for a block
// using the governance parameter blob.GovMaxSquareSize.
func (app *App) GovMaxSquareSize(ctx sdk.Context) int {
	// TODO: fix hack that forces the max square size for the first height to
	// 64. This is due to tendermint not technically supposed to be calling
	// PrepareProposal when heights are not >= 1. This is remedied in versions
	// of the sdk and comet that have full support of PreparePropsoal, although
	// celestia-app does not currently use those. see this PR for more
	// details https://github.com/cosmos/cosmos-sdk/pull/14505
	if ctx.BlockHeader().Height == 0 {
		return int(appconsts.DefaultGovMaxSquareSize)
	}

	gmax := app.BlobKeeper.GovMaxSquareSize(ctx)
	// perform a secondary check on the max square size.
	if gmax > appconsts.MaxSquareSize {
		gmax = appconsts.MaxSquareSize
	}

	return int(gmax)
}
