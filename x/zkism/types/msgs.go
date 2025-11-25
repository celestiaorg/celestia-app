package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.HasValidateBasic = (*MsgCreateInterchainSecurityModule)(nil)
	_ sdk.HasValidateBasic = (*MsgUpdateInterchainSecurityModule)(nil)
	_ sdk.HasValidateBasic = (*MsgSubmitMessages)(nil)
)

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgCreateInterchainSecurityModule) ValidateBasic() error {
	return nil
}

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgUpdateInterchainSecurityModule) ValidateBasic() error {
	if msg.Id.IsZeroAddress() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
	}

	if len(msg.Proof) != (PrefixLen + ProofSize) {
		return errorsmod.Wrapf(ErrInvalidProofLength, "expected %d, got %d", (PrefixLen + ProofSize), len(msg.Proof))
	}

	return nil
}

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgSubmitMessages) ValidateBasic() error {
	if msg.Id.IsZeroAddress() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
	}

	// TODO: Uncommented the height validation when the following is implemented:
	// https://github.com/celestiaorg/celestia-app/issues/5809
	// if msg.Height == 0 {
	// 	return errorsmod.Wrap(ErrInvalidHeight, "height must be greater than zero")
	// }

	if len(msg.Proof) != (PrefixLen + ProofSize) {
		return errorsmod.Wrapf(ErrInvalidProofLength, "expected %d, got %d", (PrefixLen + ProofSize), len(msg.Proof))
	}

	return nil
}
