package types

import (
	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/internal/groth16"
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
	if len(msg.State) < 32 {
		return errorsmod.Wrap(ErrInvalidTrustedState, "initial trusted state must be at least 32 bytes")
	}

	if len(msg.MerkleTreeAddress) != 32 {
		return errorsmod.Wrap(ErrInvalidMerkleTreeAddress, "merkle tree address must be 32 bytes")
	}

	if _, err := groth16.NewVerifyingKey(msg.Groth16Vkey); err != nil {
		return errorsmod.Wrapf(ErrInvalidVerifyingKey, "invalid groth16 verifying key")
	}

	if len(msg.StateTransitionVkey) != 32 {
		return errorsmod.Wrap(ErrInvalidVerifyingKey, "program verifying key commitment must be exactly 32 bytes")
	}

	if len(msg.StateMembershipVkey) != 32 {
		return errorsmod.Wrap(ErrInvalidVerifyingKey, "program verifying key commitment must be exactly 32 bytes")
	}

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

	if len(msg.Proof) != (PrefixLen + ProofSize) {
		return errorsmod.Wrapf(ErrInvalidProofLength, "expected %d, got %d", (PrefixLen + ProofSize), len(msg.Proof))
	}

	return nil
}
