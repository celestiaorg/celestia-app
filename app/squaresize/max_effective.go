package squaresize

import (
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxEffective returns the max effective square size.
func MaxEffective(ctx sdk.Context, blobKeeper BlobKeeper) int {
	// TODO: fix hack that forces the max square size for the first height to
	// 64. This is due to our fork of the sdk not initializing state before
	// BeginBlock of the first block. This is remedied in versions of the sdk
	// and comet that have full support of PreparePropsoal, although
	// celestia-app does not currently use those. see this PR for more details
	// https://github.com/cosmos/cosmos-sdk/pull/14505
	if ctx.BlockHeader().Height <= 1 {
		return int(appconsts.DefaultGovMaxSquareSize)
	}

	govParam := int(blobKeeper.GovMaxSquareSize(ctx))
	upperBound := appconsts.SquareSizeUpperBound(ctx.ConsensusParams().Version.AppVersion)
	return min(govParam, upperBound)
}

type BlobKeeper interface {
	GovMaxSquareSize(ctx sdk.Context) uint64
}
