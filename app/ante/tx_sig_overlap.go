package ante

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
)

// TxSigOverlapDecorator lets a new PayForFibre tx's signature verify on its own
// goroutine while the rest of the ante pass, mainly the certificate pre-pass,
// runs. It must sit right before the signature verification decorator.
type TxSigOverlapDecorator struct{}

func NewTxSigOverlapDecorator() TxSigOverlapDecorator {
	return TxSigOverlapDecorator{}
}

func (TxSigOverlapDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	if simulate || !ctx.IsCheckTx() || ctx.ExecMode() != sdk.ExecModeCheck || fibretypes.PayForFibreMsg(tx) == nil {
		return next(ctx, tx, simulate)
	}

	// A bad signature returns what the in-line check would have: its error and
	// the gas used up to it, whatever the rest of the chain did meanwhile.
	pending := &pendingTxSig{}
	defer func() {
		if r := recover(); r != nil {
			if sigErr := pending.wait(); sigErr != nil {
				newCtx, err = withGasConsumed(ctx, pending.gas), sigErr
				return
			}
			panic(r)
		}
	}()
	newCtx, err = next(ctx.WithValue(pendingTxSigKey{}, pending), tx, simulate)
	if sigErr := pending.wait(); sigErr != nil {
		return withGasConsumed(ctx, pending.gas), sigErr
	}
	return newCtx, err
}

func withGasConsumed(ctx sdk.Context, gas uint64) sdk.Context {
	if used := ctx.GasMeter().GasConsumed(); used > gas {
		ctx.GasMeter().RefundGas(used-gas, "tx signature failed before this gas was used")
	}
	return ctx
}

type pendingTxSigKey struct{}

// pendingTxSig is one tx signature verifying off the ante goroutine.
type pendingTxSig struct {
	done chan struct{}
	// gas is what the tx had used when the in-line check would have run.
	gas      uint64
	err      error
	panicked any
}

// startable reports whether sigData can verify off the ante goroutine: the
// direct sign bytes read no state, and the error below is the decorator's own.
func (p *pendingTxSig) startable(sigData signing.SignatureData) bool {
	single, ok := sigData.(*signing.SingleSignatureData)
	return p.done == nil && ok && single.SignMode == signing.SignMode_SIGN_MODE_DIRECT
}

func (p *pendingTxSig) start(gas, accNum uint64, chainID string, verify func() error) {
	p.done, p.gas = make(chan struct{}), gas
	go func() {
		defer close(p.done)
		defer func() {
			if r := recover(); r != nil {
				p.panicked = r
			}
		}()
		if err := verify(); err != nil {
			// The SDK's SigVerificationDecorator wording for a non-amino signer.
			p.err = errorsmod.Wrap(sdkerrors.ErrUnauthorized, fmt.Sprintf(
				"signature verification failed; please verify account number (%d) and chain-id (%s): (%s)",
				accNum, chainID, err.Error()))
		}
	}()
}

// wait returns the signature's verdict, re-raising a panic on the caller's goroutine.
func (p *pendingTxSig) wait() error {
	if p.done == nil {
		return nil
	}
	<-p.done
	if p.panicked != nil {
		panic(p.panicked)
	}
	return p.err
}
