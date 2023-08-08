package ante

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxBlobSizeDecorator helps to prevent a PFB from being included in a block
// but not fitting in a data square.
type MaxBlobSizeDecorator struct {
	k BlobKeeper
}

func NewMaxBlobSizeDecorator(k BlobKeeper) MaxBlobSizeDecorator {
	return MaxBlobSizeDecorator{k}
}

// AnteHandle implements the AnteHandler interface. It returns an error if the
// tx contains a MsgPayForBlobs where the total blob data size exceeds the upper
// bound. The upper bound is calculated based on the number of bytes available
// to blobs in a data square with the maximum square size.
func (d MaxBlobSizeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !ctx.IsCheckTx() {
		return next(ctx, tx, simulate)
	}

	upperBound := d.totalBlobSizeUpperBound(ctx)
	for _, m := range tx.GetMsgs() {
		if pfb, ok := m.(*blobtypes.MsgPayForBlobs); ok {
			total := getTotal(pfb.BlobSizes)
			if total > upperBound {
				return ctx, errors.Wrapf(blobtypes.ErrTotalBlobSizeTooLarge, "total blob size %d exceeds upper bound %d", total, upperBound)
			}
		}
	}

	return next(ctx, tx, simulate)
}

// totalBlobSizeUpperBound returns an upper bound for the number of bytes available
// for blob data in a data square based on the max square size. Note it is
// possible that txs with a total blob size less than this upper bound still
// fail to be included in a block due to overhead from the PFB tx and/or padding
// shares. As a result, this upper bound should only be used to reject
// transactions that are guaranteed to be too large.
func (d MaxBlobSizeDecorator) totalBlobSizeUpperBound(ctx sdk.Context) int {
	squareSize := d.getMaxSquareSize(ctx)
	totalShares := squareSize * squareSize
	blobShares := totalShares - 1 // subtract 1 to account for the PFB tx share
	return shares.AvailableBytesFromSparseShares(blobShares)
}

// getMaxSquareSize returns the maximum square size based on the current values
// for the relevant governance parameter and the versioned constant.
func (d MaxBlobSizeDecorator) getMaxSquareSize(ctx sdk.Context) int {
	// TODO: fix hack that forces the max square size for the first height to
	// 64. This is due to our fork of the sdk not initializing state before
	// BeginBlock of the first block. This is remedied in versions of the sdk
	// and comet that have full support of PreparePropsoal, although
	// celestia-app does not currently use those. see this PR for more details
	// https://github.com/cosmos/cosmos-sdk/pull/14505
	if ctx.BlockHeader().Height <= 1 {
		return int(appconsts.DefaultGovMaxSquareSize)
	}

	upperBound := appconsts.SquareSizeUpperBound(ctx.ConsensusParams().Version.AppVersion)
	govParam := d.k.GovMaxSquareSize(ctx)
	return min(upperBound, int(govParam))
}

// getTotal returns the sum of the given sizes.
func getTotal(sizes []uint32) (sum int) {
	for _, size := range sizes {
		sum += int(size)
	}
	return sum
}

// min returns the minimum of two ints. This function can be removed once we
// upgrade to Go 1.21.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
