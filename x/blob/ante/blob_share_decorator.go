package ante

import (
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BlobShareDecorator helps to prevent a PFB from being included in a block but
// not fitting in a data square because the number of shares occupied by the PFB
// exceeds the max number of shares available to blob data in a data square.
type BlobShareDecorator struct {
	k BlobKeeper
}

func NewBlobShareDecorator(k BlobKeeper) BlobShareDecorator {
	return BlobShareDecorator{k}
}

// AnteHandle implements the Cosmos SDK AnteHandler function signature. It
// returns an error if tx contains a MsgPayForBlobs where the shares occupied by
// the PFB exceeds the max number of shares in a data square.
func (d BlobShareDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !ctx.IsCheckTx() {
		return next(ctx, tx, simulate)
	}

	if ctx.BlockHeader().Version.App == v1.Version {
		return next(ctx, tx, simulate)
	}

	maxBlobShares := d.getMaxBlobShares(ctx)
	for _, m := range tx.GetMsgs() {
		if pfb, ok := m.(*blobtypes.MsgPayForBlobs); ok {
			if sharesNeeded := getSharesNeeded(pfb.BlobSizes); sharesNeeded > maxBlobShares {
				return ctx, errors.Wrapf(blobtypes.ErrBlobsTooLarge, "the number of shares occupied by blobs in this MsgPayForBlobs %d exceeds the max number of shares available for blob data %d", sharesNeeded, maxBlobShares)
			}
		}
	}

	return next(ctx, tx, simulate)
}

// getMaxBlobShares returns the max the number of shares available for blob data.
func (d BlobShareDecorator) getMaxBlobShares(ctx sdk.Context) int {
	squareSize := d.getMaxSquareSize(ctx)
	totalShares := squareSize * squareSize
	// The PFB tx share must occupy at least one share so the number of blob shares
	// is at most one less than totalShares.
	blobShares := totalShares - 1
	return blobShares
}

// getMaxSquareSize returns the maximum square size based on the current values
// for the governance parameter and the versioned constant.
func (d BlobShareDecorator) getMaxSquareSize(ctx sdk.Context) int {
	// TODO: fix hack that forces the max square size for the first height to
	// 64. This is due to our fork of the sdk not initializing state before
	// BeginBlock of the first block. This is remedied in versions of the sdk
	// and comet that have full support of PreparePropsoal, although
	// celestia-app does not currently use those. see this PR for more details
	// https://github.com/cosmos/cosmos-sdk/pull/14505
	if ctx.BlockHeader().Height <= 1 {
		return int(appconsts.DefaultGovMaxSquareSize)
	}

	upperBound := appconsts.SquareSizeUpperBound(ctx.BlockHeader().Version.App)
	govParam := d.k.GovMaxSquareSize(ctx)
	return min(upperBound, int(govParam))
}

// getSharesNeeded returns the total number of shares needed to represent all of
// the blobs described by blobSizes.
func getSharesNeeded(blobSizes []uint32) (sum int) {
	for _, blobSize := range blobSizes {
		sum += share.SparseSharesNeeded(blobSize)
	}
	return sum
}
