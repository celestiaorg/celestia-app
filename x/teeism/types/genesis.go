package types

import (
	errorsmod "cosmossdk.io/errors"
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

		if len(ism.State) != IsmStateBytes {
			return errorsmod.Wrapf(ErrInvalidTrustedState, "ism %s state must be exactly %d bytes", ism.Id.String(), IsmStateBytes)
		}
		if len(ism.MerkleTreeAddress) != 32 {
			return errorsmod.Wrapf(ErrInvalidMerkleTreeAddress, "ism %s merkle tree address must be 32 bytes", ism.Id.String())
		}
		if err := ism.Identity.Validate(); err != nil {
			return errorsmod.Wrapf(err, "ism %s", ism.Id.String())
		}

		state, err := DecodeIsmState(ism.State)
		if err != nil {
			return errorsmod.Wrapf(ErrInvalidTrustedState, "ism %s: %v", ism.Id.String(), err)
		}
		if state.IdentityDigest != ism.Identity.Digest() {
			return errorsmod.Wrapf(ErrInvalidEnclaveIdentity, "ism %s state names a different enclave than the one it pins", ism.Id.String())
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

	return nil
}
