package types

import (
	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v8/x/zkism/internal/groth16"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// DefaultGenesis returns the default module genesis.
func DefaultGenesis() *GenesisState {
	return &GenesisState{}
}

// Validate performs basic genesis state validation.
func (gs GenesisState) Validate() error {
	isms := make(map[uint64]struct{}, len(gs.Isms))
	for i := range gs.Isms {
		ism := gs.Isms[i]
		if ism.Id.IsZeroAddress() {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
		}

		internalID := ism.Id.GetInternalId()
		if _, exists := isms[internalID]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "duplicate ism id %s", ism.Id.String())
		}
		isms[internalID] = struct{}{}

		if len(ism.State) < MinStateBytes {
			return errorsmod.Wrapf(ErrInvalidTrustedState, "ism %s state must be at least %d bytes", ism.Id.String(), MinStateBytes)
		}

		if len(ism.State) > MaxStateBytes {
			return errorsmod.Wrapf(ErrInvalidTrustedState, "ism %s state must be no greater than %d bytes", ism.Id.String(), MaxStateBytes)
		}

		if len(ism.MerkleTreeAddress) != 32 {
			return errorsmod.Wrapf(ErrInvalidMerkleTreeAddress, "ism %s merkle tree address must be 32 bytes", ism.Id.String())
		}

		if _, err := groth16.NewVerifyingKey(ism.Groth16Vkey); err != nil {
			return errorsmod.Wrapf(ErrInvalidVerifyingKey, "ism %s invalid groth16 verifying key", ism.Id.String())
		}

		if len(ism.StateTransitionVkey) != 32 {
			return errorsmod.Wrapf(ErrInvalidVerifyingKey, "ism %s program verifying key commitment must be exactly 32 bytes", ism.Id.String())
		}

		if len(ism.StateMembershipVkey) != 32 {
			return errorsmod.Wrapf(ErrInvalidVerifyingKey, "ism %s program verifying key commitment must be exactly 32 bytes", ism.Id.String())
		}
	}

	messages := make(map[uint64]struct{}, len(gs.Messages))
	for i := range gs.Messages {
		msgs := gs.Messages[i]
		if msgs.Id.IsZeroAddress() {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
		}

		internalID := msgs.Id.GetInternalId()
		if _, exists := isms[internalID]; !exists {
			return errorsmod.Wrapf(ErrIsmNotFound, "messages defined for unknown ism %s", msgs.Id.String())
		}

		if _, exists := messages[internalID]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "duplicate messages entry for ism %s", msgs.Id.String())
		}
		messages[internalID] = struct{}{}

		for _, msg := range msgs.Messages {
			msgID, err := DecodeHex(msg)
			if err != nil {
				return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "invalid message id %q for ism %s: %v", msg, msgs.Id.String(), err)
			}

			if len(msgID) != 32 {
				return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "invalid message id length for ism %s: expected 32 bytes, got %d", msgs.Id.String(), len(msgID))
			}
		}
	}

	proofSubmitted := make(map[uint64]struct{}, len(gs.Submissions))
	for i := range gs.Submissions {
		entry := gs.Submissions[i]
		if entry.Id.IsZeroAddress() {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
		}

		internalID := entry.Id.GetInternalId()
		if _, exists := isms[internalID]; !exists {
			return errorsmod.Wrapf(ErrIsmNotFound, "message proof submitted entry for unknown ism %s", entry.Id.String())
		}

		if _, exists := proofSubmitted[internalID]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "duplicate message proof submitted entry for ism %s", entry.Id.String())
		}
		proofSubmitted[internalID] = struct{}{}
	}

	return nil
}
