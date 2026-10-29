package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
		if ism.Owner != "" {
			if _, err := sdk.AccAddressFromBech32(ism.Owner); err != nil {
				return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "ism %s owner: %s", ism.Id.String(), err)
			}
		}
		if err := ism.VerifierOverrides.Validate(); err != nil {
			return errorsmod.Wrapf(err, "ism %s", ism.Id.String())
		}

		state, err := DecodeIsmState(ism.State)
		if err != nil {
			return errorsmod.Wrapf(ErrInvalidTrustedState, "ism %s: %v", ism.Id.String(), err)
		}
		if state.IdentityDigest != ism.Identity.Digest() {
			return errorsmod.Wrapf(ErrInvalidEnclaveIdentity, "ism %s state names a different enclave than the one it pins", ism.Id.String())
		}
		// Empty until the first attestation, 32 bytes after it.
		if len(ism.TreeRoot) != 0 && len(ism.TreeRoot) != 32 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "ism %s tree root must be empty or 32 bytes, got %d", ism.Id.String(), len(ism.TreeRoot))
		}
	}

	roots := make(map[uint64]struct{}, len(gs.Roots))
	for i := range gs.Roots {
		entry := gs.Roots[i]
		if entry.Id.IsZeroAddress() {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
		}

		internalID := entry.Id.GetInternalId()
		if _, exists := isms[internalID]; !exists {
			return errorsmod.Wrapf(ErrIsmNotFound, "roots defined for unknown ism %s", entry.Id.String())
		}
		if _, exists := roots[internalID]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "duplicate roots entry for ism %s", entry.Id.String())
		}
		roots[internalID] = struct{}{}

		for _, root := range entry.Roots {
			raw, err := DecodeHex(root)
			if err != nil {
				return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "invalid tree root %q for ism %s: %v", root, entry.Id.String(), err)
			}
			if len(raw) != 32 {
				return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "invalid tree root length for ism %s: expected 32 bytes, got %d", entry.Id.String(), len(raw))
			}
		}
	}

	return nil
}
