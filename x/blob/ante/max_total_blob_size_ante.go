package ante

import (
	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v1"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxTotalBlobSizeDecorator helps to prevent a PFB from being included in a
// block but not fitting in a data square.
type MaxTotalBlobSizeDecorator struct {
	k BlobKeeper
}

func NewMaxTotalBlobSizeDecorator(k BlobKeeper) MaxTotalBlobSizeDecorator {
	return MaxTotalBlobSizeDecorator{k}
}

// AnteHandle implements the Cosmos SDK AnteHandler function signature. It
// returns an error if tx contains a MsgPayForBlobs where the total blob size is
// greater than the max total blob size.
func (d MaxTotalBlobSizeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !ctx.IsCheckTx() {
		return next(ctx, tx, simulate)
	}

	if ctx.BlockHeader().Version.App != v1.Version {
		return next(ctx, tx, simulate)
	}

	max := d.maxTotalBlobSize(ctx)
	for _, m := range tx.GetMsgs() {
		if pfb, ok := m.(*blobtypes.MsgPayForBlobs); ok {
			if total := getTotal(pfb.BlobSizes); total > max {
				return ctx, errors.Wrapf(blobtypes.ErrTotalBlobSizeTooLarge, "total blob size %d exceeds max %d", total, max)
			}
		}
	}

	return next(ctx, tx, simulate)
}

// maxTotalBlobSize returns the max number of bytes available for blobs in a
// data square based on the max square size. Note it is possible that txs with a
// total blob size less than this max still fail to be included in a block due
// to overhead from the PFB tx and/or padding shares.
func (d MaxTotalBlobSizeDecorator) maxTotalBlobSize(ctx sdk.Context) int {
	squareSize := d.getMaxSquareSize(ctx)
	totalShares := squareSize * squareSize
	// The PFB tx share must occupy at least one share so the # of blob shares
	// is at least one less than totalShares.
	blobShares := totalShares - 1
	return share.AvailableBytesFromSparseShares(blobShares)
}

// getMaxSquareSize returns the maximum square size based on the current values
// for the relevant governance parameter and the versioned constant.
func (d MaxTotalBlobSizeDecorator) getMaxSquareSize(ctx sdk.Context) int {
	// TODO: fix hack that forces the max square size for the first height to
	// 64. This is due to our fork of the sdk not initializing state before
	// BeginBlock of the first block. This is remedied in versions of the sdk
	// and comet that have full support of PrepareProposal, although
	// celestia-app does not currently use those. see this PR for more details
	// https://github.com/cosmos/cosmos-sdk/pull/14505
	if ctx.BlockHeader().Height <= 1 {
		return int(appconsts.DefaultGovMaxSquareSize)
	}

	upperBound := appconsts.SquareSizeUpperBound(ctx.BlockHeader().Version.App)
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
