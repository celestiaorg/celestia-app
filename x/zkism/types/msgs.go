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
	_ sdk.HasValidateBasic = (*MsgUpdateVerifyingKeys)(nil)
)

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgCreateInterchainSecurityModule) ValidateBasic() error {
	if len(msg.State) < MinStateBytes {
		return errorsmod.Wrapf(ErrInvalidTrustedState, "initial trusted state must be at least %d bytes", MinStateBytes)
	}

	if len(msg.State) > MaxStateBytes {
		return errorsmod.Wrapf(ErrInvalidTrustedState, "initial trusted state must be no greater than %d bytes", MaxStateBytes)
	}

	if len(msg.MerkleTreeAddress) != 32 {
		return errorsmod.Wrap(ErrInvalidMerkleTreeAddress, "merkle tree address must be 32 bytes")
	}

	return validateVerifyingKeys(msg.Groth16Vkey, msg.StateTransitionVkey, msg.StateMembershipVkey)
}

// validateVerifyingKeys validates the groth16 verifying key and the program verifying key commitments of an ism.
// Only cheap structural checks run here; deserializing the key decompresses
// elliptic curve points, so the message handler does it after fees and gas apply.
func validateVerifyingKeys(groth16Vkey, stateTransitionVkey, stateMembershipVkey []byte) error {
	if err := ValidateGroth16Vkey(groth16Vkey); err != nil {
		return errorsmod.Wrapf(ErrInvalidVerifyingKey, "%v", err)
	}

	if len(stateTransitionVkey) != 32 {
		return errorsmod.Wrap(ErrInvalidVerifyingKey, "program verifying key commitment must be exactly 32 bytes")
	}

	if len(stateMembershipVkey) != 32 {
		return errorsmod.Wrap(ErrInvalidVerifyingKey, "program verifying key commitment must be exactly 32 bytes")
	}

	return nil
}

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgUpdateInterchainSecurityModule) ValidateBasic() error {
	if msg.Id.IsZeroAddress() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
	}

	if len(msg.Proof) != ProofBytesLen {
		return errorsmod.Wrapf(ErrInvalidProofLength, "expected %d, got %d", ProofBytesLen, len(msg.Proof))
	}

	if len(msg.PublicValues) > MaxStateTransitionValuesBytes {
		return errorsmod.Wrapf(ErrInvalidPublicValuesLength, "public values must not exceed %d bytes", MaxStateTransitionValuesBytes)
	}

	return nil
}

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgSubmitMessages) ValidateBasic() error {
	if msg.Id.IsZeroAddress() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
	}

	if len(msg.Proof) != ProofBytesLen {
		return errorsmod.Wrapf(ErrInvalidProofLength, "expected %d, got %d", ProofBytesLen, len(msg.Proof))
	}

	if len(msg.PublicValues) != StateMembershipValuesBytes {
		return errorsmod.Wrapf(ErrInvalidPublicValuesLength, "public values must be exactly %d bytes", StateMembershipValuesBytes)
	}

	return nil
}

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgUpdateVerifyingKeys) ValidateBasic() error {
	if msg.Id.IsZeroAddress() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
	}

	return validateVerifyingKeys(msg.Groth16Vkey, msg.StateTransitionVkey, msg.StateMembershipVkey)
}
