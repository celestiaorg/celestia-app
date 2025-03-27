package ante

import (
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// MinGasPFBDecorator helps to prevent a PFB from being included in a block
// but running out of gas in DeliverTx (effectively getting DA for free)
// This decorator should be run after any decorator that consumes gas.
type MinGasPFBDecorator struct {
	k BlobKeeper
}

func NewMinGasPFBDecorator(k BlobKeeper) MinGasPFBDecorator {
	return MinGasPFBDecorator{k}
}

// AnteHandle implements the AnteHandler interface. It checks to see
// if the transaction contains a MsgPayForBlobs and if so, checks that
// the transaction has allocated enough gas.
func (d MinGasPFBDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if ctx.IsReCheckTx() {
		return next(ctx, tx, simulate)
	}

	gasPerByte := d.getGasPerByte(ctx)
	txGas := ctx.GasMeter().GasRemaining()
	err := d.validatePFBHasEnoughGas(tx.GetMsgs(), gasPerByte, txGas)
	if err != nil {
		return ctx, err
	}

	return next(ctx, tx, simulate)
}

func (d MinGasPFBDecorator) validatePFBHasEnoughGas(msgs []sdk.Msg, gasPerByte uint32, txGas uint64) error {
	for _, m := range msgs {
		if execMsg, ok := m.(*authz.MsgExec); ok {
			// Recursively look for PFBs in nested authz messages.
			nestedMsgs, err := execMsg.GetMessages()
			if err != nil {
				return err
			}
			err = d.validatePFBHasEnoughGas(nestedMsgs, gasPerByte, txGas)
			if err != nil {
				return err
			}
		}
		// NOTE: here we assume only one PFB per transaction
		if pfb, ok := m.(*types.MsgPayForBlobs); ok {
			err := validateEnoughGas(pfb, gasPerByte, txGas)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (d MinGasPFBDecorator) getGasPerByte(ctx sdk.Context) uint32 {
	if ctx.BlockHeader().Version.App <= v2.Version {
		return d.k.GasPerBlobByte(ctx)
	}
	return appconsts.GasPerBlobByte(ctx.BlockHeader().Version.App)
}

// validateEnoughGas checks if the pfb has enough gas to pay for the PFB.
func validateEnoughGas(pfb *types.MsgPayForBlobs, gasPerByte uint32, txGas uint64) error {
	gasToConsume := pfb.Gas(gasPerByte)
	if gasToConsume > txGas {
		return errors.Wrapf(sdkerrors.ErrInsufficientFee, "not enough gas to pay for blobs (minimum: %d, got: %d)", gasToConsume, txGas)
	}
	return nil
}

type BlobKeeper interface {
	GasPerBlobByte(ctx sdk.Context) uint32
	GovMaxSquareSize(ctx sdk.Context) uint64
}
