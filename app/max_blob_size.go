package app

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxBlobSize returns an upper bound for the maximum blob size that can fit in
// a single data square. Since the returned value is an upper bound, it is
// possible that slightly smaller blob may not fit due to shares that aren't
// occupied by the blob (i.e. the PFB tx shares).
func (app *App) MaxBlobSize(ctx sdk.Context) int {
	maxSquareSize := app.GovSquareSizeUpperBound(ctx)
	maxShares := maxSquareSize * maxSquareSize
	maxShareBytes := maxShares * appconsts.ContinuationSparseShareContentSize

	// TODO(rootulp): get MaxBytes consensus params from core
	maxBlockBytes := appconsts.DefaultMaxBytes

	return min(maxShareBytes, maxBlockBytes)
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
