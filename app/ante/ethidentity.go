package ante

import (
	"fmt"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// EthIdentityKeeper records observed same-key Ethereum and Celestia identity
// mappings.
type EthIdentityKeeper interface {
	IndexPubKey(ctx sdk.Context, pubKey cryptotypes.PubKey) error
}

// EthIdentityIndexDecorator lazily indexes observed signer public keys.
type EthIdentityIndexDecorator struct {
	ak                sdkante.AccountKeeper
	ethIdentityKeeper EthIdentityKeeper
}

// NewEthIdentityIndexDecorator creates an ante decorator that records
// Ethereum identity mappings after signer public keys have been set.
func NewEthIdentityIndexDecorator(ak sdkante.AccountKeeper, ethIdentityKeeper EthIdentityKeeper) EthIdentityIndexDecorator {
	return EthIdentityIndexDecorator{
		ak:                ak,
		ethIdentityKeeper: ethIdentityKeeper,
	}
}

// AnteHandle indexes signer public keys only during finalized execution so
// CheckTx, PrepareProposal, and ProcessProposal cannot write divergent state.
func (d EthIdentityIndexDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if simulate || d.ethIdentityKeeper == nil || ctx.ExecMode() != sdk.ExecModeFinalize {
		return next(ctx, tx, simulate)
	}

	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, fmt.Errorf("invalid tx type: expected SigVerifiableTx, got %T", tx)
	}

	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}

	for _, signer := range signers {
		acc, err := sdkante.GetSignerAcc(ctx, d.ak, signer)
		if err != nil {
			return ctx, err
		}
		if pubKey := acc.GetPubKey(); pubKey != nil {
			if err := d.ethIdentityKeeper.IndexPubKey(ctx, pubKey); err != nil {
				return ctx, fmt.Errorf("failed to index Ethereum identity for signer %s: %w", sdk.AccAddress(signer), err)
			}
		}
	}

	return next(ctx, tx, simulate)
}
