package types

import (
	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/internal/groth16"
	"github.com/celestiaorg/go-square/v2/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.HasValidateBasic = (*MsgCreateZKExecutionISM)(nil)
	_ sdk.HasValidateBasic = (*MsgUpdateZKExecutionISM)(nil)
	_ sdk.HasValidateBasic = (*MsgSubmitMessages)(nil)
)

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgCreateZKExecutionISM) ValidateBasic() error {
	if _, err := share.NewNamespaceFromBytes(msg.Namespace); err != nil {
		return errorsmod.Wrapf(ErrInvalidNamespace, "failed to parse namespace from bytes: %x", msg.Namespace)
	}

	if len(msg.SequencerPublicKey) != 32 {
		return errorsmod.Wrap(ErrInvalidSequencerKey, "public must be exactly 32 bytes")
	}

	if len(msg.StateRoot) != 32 {
		return errorsmod.Wrap(ErrInvalidStateRoot, "state root must be exactly 32 bytes")
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
func (msg *MsgUpdateZKExecutionISM) ValidateBasic() error {
	if msg.Id.IsZeroAddress() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
	}

	if msg.Height == 0 {
		return errorsmod.Wrap(ErrInvalidHeight, "height must be greater than zero")
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
