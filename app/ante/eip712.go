package ante

import (
	"bytes"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v9/pkg/tx/eip712"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// EIP712PubKeyRecoveryGas is the gas charged for recovering an EIP-712 signer
// public key during ante handling.
const EIP712PubKeyRecoveryGas storetypes.Gas = 1000

// EIP712ExtensionOptionChecker reports whether opt is the supported Celestia
// EIP-712 critical extension option.
func EIP712ExtensionOptionChecker(opt *codectypes.Any) bool {
	return eip712.ExtensionOptionChecker(opt)
}

// EIP712SetPubKeyDecorator recovers and stores the signer public key for
// EIP-712 transactions when the account does not already have one.
type EIP712SetPubKeyDecorator struct {
	ak ante.AccountKeeper
}

// NewEIP712SetPubKeyDecorator returns an ante decorator that handles EIP-712
// public key recovery before signature verification.
func NewEIP712SetPubKeyDecorator(ak ante.AccountKeeper) EIP712SetPubKeyDecorator {
	return EIP712SetPubKeyDecorator{ak: ak}
}

// AnteHandle recovers the public key for a single-signer EIP-712 transaction,
// verifies it maps to the canonical signer address, and stores it on the
// account if needed.
func (d EIP712SetPubKeyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if simulate || !ctx.IsSigverifyTx() {
		return next(ctx, tx, simulate)
	}

	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}

	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return ctx, err
	}

	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}

	if !hasEIP712Signature(sigs) {
		return next(ctx, tx, simulate)
	}

	if len(sigs) != 1 || len(signers) != 1 {
		return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "EIP-712 supports exactly one signer")
	}

	if !eip712.IsEIP712SignatureData(sigs[0].Data) {
		return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "mixed EIP-712 signer configuration is unsupported")
	}

	acc, err := ante.GetSignerAcc(ctx, d.ak, signers[0])
	if err != nil {
		return ctx, err
	}

	adaptableTx, ok := tx.(authsigning.V2AdaptableTx)
	if !ok {
		return ctx, fmt.Errorf("expected tx to implement V2AdaptableTx, got %T", tx)
	}

	signer, err := d.ak.AddressCodec().BytesToString(signers[0])
	if err != nil {
		return ctx, err
	}

	signerData := txsigning.SignerData{
		Address:       signer,
		ChainID:       ctx.ChainID(),
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
	}

	single := sigs[0].Data.(*signingtypes.SingleSignatureData)
	ctx.GasMeter().ConsumeGas(EIP712PubKeyRecoveryGas, "ante verify: eip712 pubkey recovery")
	recovered, err := eip712.RecoverPubKey(signerData, adaptableTx.GetSigningTxData(), single.Signature)
	if err != nil {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "EIP-712 pubkey recovery failed: %s", err)
	}

	if string(recovered.Address()) != string(signers[0]) {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "EIP-712 recovered signer %s does not match canonical signer %s", sdk.AccAddress(recovered.Address()), signer)
	}

	if existing := acc.GetPubKey(); existing != nil {
		if !pubKeysEqual(existing, recovered) {
			return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "EIP-712 recovered pubkey does not match account pubkey")
		}
		return next(ctx, tx, simulate)
	}

	if err := acc.SetPubKey(recovered); err != nil {
		return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, err.Error())
	}
	d.ak.SetAccount(ctx, acc)

	return next(ctx, tx, simulate)
}

// EIP712ValidateSigCountDecorator enforces Phase 1 EIP-712 signature count
// limits while delegating non-EIP-712 transactions to the SDK decorator.
type EIP712ValidateSigCountDecorator struct {
	ak ante.AccountKeeper
}

// NewEIP712ValidateSigCountDecorator returns an ante decorator that validates
// signature count rules for EIP-712 transactions.
func NewEIP712ValidateSigCountDecorator(ak ante.AccountKeeper) EIP712ValidateSigCountDecorator {
	return EIP712ValidateSigCountDecorator{ak: ak}
}

// AnteHandle rejects EIP-712 transactions that are not exactly one single
// signature and applies the account keeper transaction signature limit.
func (d EIP712ValidateSigCountDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a sigTx")
	}

	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return ctx, err
	}

	if !hasEIP712Signature(sigs) {
		return ante.NewValidateSigCountDecorator(d.ak).AnteHandle(ctx, tx, simulate, next)
	}

	if len(sigs) != 1 || !eip712.IsEIP712SignatureData(sigs[0].Data) {
		return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "EIP-712 supports exactly one single signature")
	}

	if d.ak.GetParams(ctx).TxSigLimit < 1 {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTooManySignatures, "signatures: 1, limit: 0")
	}

	return next(ctx, tx, simulate)
}

// hasEIP712Signature reports whether any signature in sigs uses
// SIGN_MODE_EIP_712.
func hasEIP712Signature(sigs []signingtypes.SignatureV2) bool {
	for _, sig := range sigs {
		if eip712.IsEIP712SignatureData(sig.Data) {
			return true
		}
	}
	return false
}

// pubKeysEqual reports whether the recovered EIP-712 public key matches the
// account public key.
func pubKeysEqual(left cryptotypes.PubKey, right *secp256k1.PubKey) bool {
	return left != nil && right != nil && bytes.Equal(left.Bytes(), right.Bytes())
}
