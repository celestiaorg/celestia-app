package types

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/go-square/v2/share"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
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
	// Validate signer_public_key is a valid public key
	if msg.SignerPublicKey == nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "signer public key cannot be nil")
	}

	pubKey, ok := msg.SignerPublicKey.GetCachedValue().(cryptotypes.PubKey)
	if !ok {
		return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "failed to get cached public key")
	}

	if pubKey == nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "signer public key cannot be nil")
	}

	// Validate namespace
	if len(msg.Namespace) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "namespace cannot be empty")
	}

	// Validate namespace format (29 bytes total)
	if len(msg.Namespace) != share.NamespaceSize {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "namespace must be %d bytes, got %d", share.NamespaceSize, len(msg.Namespace))
	}

	// Parse and validate namespace
	ns, err := share.NewNamespaceFromBytes(msg.Namespace)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid namespace: %s", err)
	}

	// Check if namespace is reserved (not allowed for user blobs)
	if err := ValidateBlobNamespace(ns); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid blob namespace: %s", err)
	}

	// Validate blob_size is positive
	if msg.BlobSize == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "blob size must be positive")
	}

	// Validate commitment is 32 bytes
	if len(msg.Commitment) != 32 {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "commitment must be 32 bytes, got %d", len(msg.Commitment))
	}

	// Validate row_version is supported
	if msg.RowVersion != uint32(share.ShareVersionZero) && msg.RowVersion != uint32(share.ShareVersionOne) {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "unsupported row version: %d", msg.RowVersion)
	}

	// Validate height is positive
	if msg.Height <= 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "height must be positive")
	}

	// Validate creation_timestamp is not zero
	if msg.CreationTimestamp.IsZero() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "creation timestamp cannot be zero")
	}

	// Validate signature is properly formatted and non-empty
	if len(msg.Signature) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "signature cannot be empty")
	}

	// Validate chain_id is not empty
	if msg.ChainId == "" {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "chain ID cannot be empty")
	}

	return nil
}

// ValidateBasic performs stateless validation for MsgPayForFibre
func (msg *MsgPayForFibre) ValidateBasic() error {
	// Validate signer address
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer address: %s", err)
	}

	// Validate PaymentPromise
	if err := msg.PaymentPromise.ValidateBasic(); err != nil {
		return errorsmod.Wrap(err, "invalid payment promise")
	}

	// Must have at least one validator signature
	if len(msg.ValidatorSignatures) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "must have at least one validator signature")
	}

	// All validator signatures must be properly formatted (non-empty)
	for i, sig := range msg.ValidatorSignatures {
		if len(sig) == 0 {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "validator signature at index %d cannot be empty", i)
		}
	}

	return nil
}

// ValidateBasic performs stateless validation for MsgPaymentPromiseTimeout
func (msg *MsgPaymentPromiseTimeout) ValidateBasic() error {
	// Validate signer address (can be anyone)
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer address: %s", err)
	}

	// Validate PaymentPromise (including signature validation)
	if err := msg.PaymentPromise.ValidateBasic(); err != nil {
		return errorsmod.Wrap(err, "invalid payment promise")
	}

	return nil
}

// ValidateBasic performs stateless validation for MsgUpdateFibreParams
func (msg *MsgUpdateFibreParams) ValidateBasic() error {
	// Validate authority address
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority address: %s", err)
	}

	// Validate params
	if err := msg.Params.Validate(); err != nil {
		return errorsmod.Wrap(err, "invalid params")
	}

	return nil
}

// ValidateBlobNamespace validates that a namespace is suitable for blob data
// This is adapted from the blob module's validation logic
func ValidateBlobNamespace(ns share.Namespace) error {
	if ns.IsReserved() {
		return fmt.Errorf("namespace %s is reserved", ns)
	}

	// Additional validation can be added here if needed
	// The IsReserved() check above covers the main validation requirements

	return nil
}
