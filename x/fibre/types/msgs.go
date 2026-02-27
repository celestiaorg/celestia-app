package types

import (
	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	BlobVersionZero = uint32(0)
)

// ValidateBasic performs stateless validation for MsgDepositToEscrow
func (msg *MsgDepositToEscrow) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer address: %s", err)
	}

	if !msg.Amount.IsValid() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidCoins, msg.Amount.String())
	}

	if !msg.Amount.IsPositive() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidCoins, "amount must be positive")
	}

	return nil
}

// ValidateBasic performs stateless validation for MsgRequestWithdrawal
func (msg *MsgRequestWithdrawal) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer address: %s", err)
	}

	if !msg.Amount.IsValid() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidCoins, msg.Amount.String())
	}

	if !msg.Amount.IsPositive() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidCoins, "amount must be positive")
	}

	return nil
}

// ValidateBasic performs stateless validation for PaymentPromise
func (msg *PaymentPromise) ValidateBasic() error {
	if msg.ChainId == "" {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "chain ID cannot be empty")
	}

	if len(msg.Namespace) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "namespace cannot be empty")
	}

	if len(msg.Namespace) != share.NamespaceSize {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "namespace must be %d bytes, got %d", share.NamespaceSize, len(msg.Namespace))
	}

	namespace, err := share.NewNamespaceFromBytes(msg.Namespace)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid namespace: %s", err)
	}

	if err := namespace.ValidateForBlob(); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid blob namespace: %s", err)
	}

	if msg.BlobSize == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "blob size must be positive")
	}

	if len(msg.Commitment) != 32 {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "commitment must be 32 bytes, got %d", len(msg.Commitment))
	}

	if err := validateBlobVersion(msg.BlobVersion); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid blob version: %s", err)
	}

	if msg.Height <= 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "height must be positive")
	}

	if msg.CreationTimestamp.IsZero() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "creation timestamp cannot be zero")
	}

	if len(msg.SignerPublicKey.Key) != secp256k1.PubKeySize {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey, "signer public key must be %d bytes, got %d", secp256k1.PubKeySize, len(msg.SignerPublicKey.Key))
	}

	if len(msg.Signature) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "signature cannot be empty")
	}

	return nil
}

// ValidateBasic performs stateless validation for MsgPayForFibre
func (msg *MsgPayForFibre) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer address: %s", err)
	}

	if err := msg.PaymentPromise.ValidateBasic(); err != nil {
		return errorsmod.Wrap(err, "invalid payment promise")
	}

	if len(msg.ValidatorSignatures) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "must have at least one validator signature")
	}

	return nil
}

// ValidateBasic performs stateless validation for MsgPaymentPromiseTimeout
func (msg *MsgPaymentPromiseTimeout) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer address: %s", err)
	}

	if err := msg.PaymentPromise.ValidateBasic(); err != nil {
		return errorsmod.Wrap(err, "invalid payment promise")
	}

	return nil
}

// ValidateBasic performs stateless validation for MsgUpdateFibreParams
func (msg *MsgUpdateFibreParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority address: %s", err)
	}

	if err := msg.Params.Validate(); err != nil {
		return errorsmod.Wrap(err, "invalid params")
	}

	return nil
}

func validateBlobVersion(blobVersion uint32) error {
	if blobVersion != BlobVersionZero {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "unsupported blob version: %d", blobVersion)
	}
	return nil
}
