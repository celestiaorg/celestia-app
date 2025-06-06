package ante

import (
	"fmt"

	txsigning "cosmossdk.io/x/tx/signing"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// SafeSigVerificationDecorator wraps the standard SigVerificationDecorator
// with additional validation to prevent panics from nil PubKey in signatures
type SafeSigVerificationDecorator struct {
	decorator ante.SigVerificationDecorator
}

// NewSafeSigVerificationDecorator creates a new SafeSigVerificationDecorator that wraps
// the standard SigVerificationDecorator with nil PubKey validation
func NewSafeSigVerificationDecorator(ak ante.AccountKeeper, signModeHandler *txsigning.HandlerMap) SafeSigVerificationDecorator {
	return SafeSigVerificationDecorator{
		decorator: ante.NewSigVerificationDecorator(ak, signModeHandler),
	}
}

func (svd SafeSigVerificationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// Validate that all signatures have non-nil PubKeys before proceeding
	if err := validateSignaturePubKeys(tx); err != nil {
		return ctx, err
	}

	// If validation passes, delegate to the standard decorator
	return svd.decorator.AnteHandle(ctx, tx, simulate, next)
}

// validateSignaturePubKeys checks that all signatures in the transaction have non-nil PubKeys
func validateSignaturePubKeys(tx sdk.Tx) error {
	sigTx, ok := tx.(authsigning.Tx)
	if !ok {
		return fmt.Errorf("tx does not implement Tx interface")
	}

	signatures, err := sigTx.GetSignaturesV2()
	if err != nil {
		return fmt.Errorf("failed to get signatures: %w", err)
	}

	for i, sig := range signatures {
		if sig.PubKey == nil {
			return fmt.Errorf("signature at index %d has nil PubKey", i)
		}
	}

	return nil
}