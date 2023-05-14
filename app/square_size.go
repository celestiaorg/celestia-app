package app

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/square"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GovMaxSquareSize returns the maximum square size that can be used for a block
// using the max bytes value from the consensus params. Governance can change
// the result of this value by changing the MaxBytes consensus parameter.
func (app *App) GovMaxSquareSize(ctx sdk.Context) int {
	// TODO: fix hack that forces the max square size for the first height to
	// 64. This is due to the sdk not loading state until calling commit for
	// height 1. This means that even if the actual GovMaxSquareSize is supposed
	// to be something other than 64, it will be 64 for the first block
	// prodcuced.
	if ctx.BlockHeader().Height == 0 {
		return int(SquareSizeFromMaxBytes(int64(appconsts.DefaultMaxBytes)))
	}
	params := app.GetConsensusParams(ctx)
	return int(SquareSizeFromMaxBytes(params.Block.MaxBytes))
}

// SquareSizeFromMaxBytes returns the square size that will be used for a given
// max bytes value. It does not account for any encoding overhead. It will
// return the hardcoded appconsts.MaxSquareSize if the size is greater than
// that.
func SquareSizeFromMaxBytes(mbytes int64) uint64 {
	sharesUsed := mbytes / appconsts.ContinuationSparseShareContentSize
	size := square.Size(int(sharesUsed))
	if size > appconsts.MaxSquareSize {
		size = appconsts.MaxSquareSize
	}
	return size
}
