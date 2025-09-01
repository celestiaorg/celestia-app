package ante

import (
	"math"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
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

	txBytes := ctx.TxBytes()
	if len(txBytes) > math.MaxUint32 {
		return ctx, errors.Wrapf(blobtypes.ErrBlobsTooLarge, "the tx size %d exceeds the max uint32", txBytes)
	}
	txSize := uint32(len(txBytes))
	maxBlobShares := d.getMaxBlobShares(ctx)

	err := d.validateMsgs(tx.GetMsgs(), txSize, maxBlobShares)
	if err != nil {
		return ctx, err
	}

	return next(ctx, tx, simulate)
}

func (d BlobShareDecorator) validateMsgs(msgs []sdk.Msg, txSize uint32, maxBlobShares int) error {
	for _, m := range msgs {
		if execMsg, ok := m.(*authz.MsgExec); ok {
			// Recursively look for PFBs in nested authz messages.
			nestedMsgs, err := execMsg.GetMessages()
			if err != nil {
				return err
			}
			err = d.validateMsgs(nestedMsgs, txSize, maxBlobShares)
			if err != nil {
				return err
			}
		}

		if pfb, ok := m.(*blobtypes.MsgPayForBlobs); ok {
			if sharesNeeded := getSharesNeeded(txSize, pfb.BlobSizes); sharesNeeded > maxBlobShares {
				return errors.Wrapf(blobtypes.ErrBlobsTooLarge, "the number of shares occupied by blobs in this MsgPayForBlobs %d exceeds the max number of shares available for blob data %d", sharesNeeded, maxBlobShares)
			}
		}
	}
	return nil
}

// getMaxBlobShares returns the max the number of shares available for blob data.
func (d BlobShareDecorator) getMaxBlobShares(ctx sdk.Context) int {
	squareSize := d.getMaxSquareSize(ctx)
	totalShares := squareSize * squareSize
	// the shares used up by the tx are calculated in `getSharesNeeded`
	return totalShares
}

// getMaxSquareSize returns the max effective square size.
func (d BlobShareDecorator) getMaxSquareSize(ctx sdk.Context) int {
	govMax := d.k.GetParams(ctx).GovMaxSquareSize
	hardMax := appconsts.GetSquareSizeUpperBound(ctx.ChainID())
	return min(int(govMax), hardMax)
}

// getSharesNeeded returns the total number of shares needed to represent all of
// the blobs described by blobSizes along with the shares used by the tx
func getSharesNeeded(txSize uint32, blobSizes []uint32) (sum int) {
	sum = share.CompactSharesNeeded(txSize)
	for _, blobSize := range blobSizes {
		sum += share.SparseSharesNeeded(blobSize)
	}
	return sum
}
