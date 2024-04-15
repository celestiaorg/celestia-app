package ante

import (
	"github.com/celestiaorg/celestia-app/v2/app/squaresize"
	blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	"github.com/celestiaorg/go-square/shares"

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
	squareSize := squaresize.MaxEffective(ctx, d.k)
	totalShares := squareSize * squareSize
	// The PFB tx share must occupy at least one share so the number of blob shares
	// is at most one less than totalShares.
	blobShares := totalShares - 1
	return blobShares
}

// getSharesNeeded returns the total number of shares needed to represent all of
// the blobs described by blobSizes.
func getSharesNeeded(blobSizes []uint32) (sum int) {
	for _, blobSize := range blobSizes {
		sum += shares.SparseSharesNeeded(blobSize)
	}
	return sum
}
