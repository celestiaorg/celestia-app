package ante

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	txsigning "cosmossdk.io/x/tx/signing"
	txethereum "github.com/celestiaorg/celestia-app/v9/pkg/tx/ethereum"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// CelestiaExtensionOptionChecker reports whether opt is one of the supported
// Celestia critical extension options.
func CelestiaExtensionOptionChecker(opt *codectypes.Any) bool {
	return EIP712ExtensionOptionChecker(opt) || txethereum.ExtensionOptionChecker(opt)
}

// EthereumTxAuthorizationDecorator enforces the critical extension option and
// sign-mode pairing for SIGN_MODE_ETHEREUM_TX transactions.
type EthereumTxAuthorizationDecorator struct {
	ak                ante.AccountKeeper
	ethIdentityKeeper EthIdentityKeeper
}

// NewEthereumTxAuthorizationDecorator returns an ante decorator that validates
// translated Ethereum transaction authorization data before stock signature
// verification runs.
func NewEthereumTxAuthorizationDecorator(ak ante.AccountKeeper, ethIdentityKeeper EthIdentityKeeper) EthereumTxAuthorizationDecorator {
	return EthereumTxAuthorizationDecorator{
		ak:                ak,
		ethIdentityKeeper: ethIdentityKeeper,
	}
}

// AnteHandle rejects partial Ethereum transaction authorization state and
// verifies that the translated native transaction matches the preserved
// Ethereum envelope.
func (d EthereumTxAuthorizationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}

	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return ctx, err
	}

	hasEthereumSig := hasEthereumTxSignature(sigs)
	hasEthereumExt := hasEthereumTxExtension(tx)
	if !hasEthereumSig && !hasEthereumExt {
		return next(ctx, tx, simulate)
	}
	if hasEthereumSig != hasEthereumExt {
		return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "SIGN_MODE_ETHEREUM_TX requires ExtensionOptionsEthereumTx and vice versa")
	}
	if hasEIP712Signature(sigs) {
		return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "SIGN_MODE_ETHEREUM_TX cannot be combined with SIGN_MODE_EIP_712")
	}
	if len(sigs) != 1 || !txethereum.IsEthereumTxSignatureData(sigs[0].Data) {
		return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "SIGN_MODE_ETHEREUM_TX supports exactly one single signature")
	}

	adaptableTx, ok := tx.(authsigning.V2AdaptableTx)
	if !ok {
		return ctx, fmt.Errorf("expected tx to implement V2AdaptableTx, got %T", tx)
	}
	txData := adaptableTx.GetSigningTxData()

	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}
	if len(signers) != 1 {
		return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "SIGN_MODE_ETHEREUM_TX supports exactly one signer")
	}

	acc, err := ante.GetSignerAcc(ctx, d.ak, signers[0])
	if err != nil {
		return ctx, err
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
	if err := txethereum.ValidateValueTransfer(ctx, d.ethIdentityKeeper, signerData, txData, single.Signature); err != nil {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "Ethereum transaction authorization failed: %s", err)
	}

	return next(ctx, tx, simulate)
}

func hasEthereumTxSignature(sigs []signingtypes.SignatureV2) bool {
	for _, sig := range sigs {
		if txethereum.IsEthereumTxSignatureData(sig.Data) {
			return true
		}
	}
	return false
}

func hasEthereumTxExtension(tx sdk.Tx) bool {
	hasExtOptsTx, ok := tx.(ante.HasExtensionOptionsTx)
	if !ok {
		return false
	}
	for _, opt := range hasExtOptsTx.GetExtensionOptions() {
		if opt.TypeUrl == txethereum.ExtensionOptionsTypeURL {
			return true
		}
	}
	return false
}
