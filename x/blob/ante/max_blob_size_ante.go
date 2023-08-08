package ante

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
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
// tx contains a MsgPayForBlobs where the total blob data size exceeds the max
// total blob size.
func (d MaxBlobSizeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !ctx.IsCheckTx() {
		return next(ctx, tx, simulate)
	}

	max := appconsts.MaxTotalBlobSize(ctx.ConsensusParams().Version.AppVersion)
	for _, m := range tx.GetMsgs() {
		if pfb, ok := m.(*blobtypes.MsgPayForBlobs); ok {
			total := getTotal(pfb.BlobSizes)
			if total > max {
				return ctx, errors.Wrapf(blobtypes.ErrTotalBlobSizeTooLarge, "tx total blob size %d exceeds max total blob size %d", total, max)
			}
		}
	}

	return next(ctx, tx, simulate)
}

// getTotal returns the sum of the given sizes.
func getTotal(sizes []uint32) (sum int) {
	for _, size := range sizes {
		sum += int(size)
	}
	return sum
}
