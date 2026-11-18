package types

import (
	"bytes"

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
	isms := make(map[uint64]InterchainSecurityModule, len(gs.Isms))
	for i := range gs.Isms {
		ism := gs.Isms[i]
		if ism.Id.IsZeroAddress() {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
		}

		internalID := ism.Id.GetInternalId()
		if _, exists := isms[internalID]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "duplicate ism id %s", ism.Id.String())
		}
		isms[internalID] = ism

		if err := validateGenesisIsm(ism); err != nil {
			return err
		}
	}

	roots := make(map[uint64]struct{}, len(gs.Roots))
	for i := range gs.Roots {
		entry := gs.Roots[i]
		if entry.Id.IsZeroAddress() {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
		}

		internalID := entry.Id.GetInternalId()
		ism, exists := isms[internalID]
		if !exists {
			return errorsmod.Wrapf(ErrIsmNotFound, "roots defined for unknown ism %s", entry.Id.String())
		}
		if _, exists := roots[internalID]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "duplicate roots entry for ism %s", entry.Id.String())
		}
		roots[internalID] = struct{}{}

		if err := validateGenesisRoots(entry, ism); err != nil {
			return err
		}
	}

	// An ism that has attested keeps at least its newest root, so it needs an
	// entry; one with roots above was checked there.
	for id, ism := range isms {
		if _, listed := roots[id]; !listed && ism.RootSeq > 0 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "ism %s has attested %d roots but genesis lists none", ism.Id.String(), ism.RootSeq)
		}
	}

	return nil
}

// validateGenesisIsm checks one ism's own fields, everything but its id.
func validateGenesisIsm(ism InterchainSecurityModule) error {
	if len(ism.State) != IsmStateBytes {
		return errorsmod.Wrapf(ErrInvalidTrustedState, "ism %s state must be exactly %d bytes", ism.Id.String(), IsmStateBytes)
	}
	if len(ism.MerkleTreeAddress) != 32 {
		return errorsmod.Wrapf(ErrInvalidMerkleTreeAddress, "ism %s merkle tree address must be 32 bytes", ism.Id.String())
	}
	digest, err := IdentitySetDigest(ism.Identities)
	if err != nil {
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
	if state.IdentityDigest != digest {
		return errorsmod.Wrapf(ErrInvalidEnclaveIdentity, "ism %s state names different enclaves than the ones it pins", ism.Id.String())
	}
	// Empty until the first attestation, 32 bytes after it.
	if len(ism.TreeRoot) != 0 && len(ism.TreeRoot) != 32 {
		return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "ism %s tree root must be empty or 32 bytes, got %d", ism.Id.String(), len(ism.TreeRoot))
	}
	if ism.RootRetention != 0 {
		if err := ValidateRootRetention(ism.RootRetention); err != nil {
			return errorsmod.Wrapf(err, "ism %s", ism.Id.String())
		}
	}
	return nil
}

// validateGenesisRoots checks that every root of one entry is a 32-byte hex value.
//
// The roots are the ism's retention window, oldest first: exactly the newest
// min(root_seq, retention) roots it attested, distinct, ending with its tree root.
func validateGenesisRoots(entry GenesisRoots, ism InterchainSecurityModule) error {
	if want := min(ism.RootSeq, ism.Retention()); uint64(len(entry.Roots)) != want {
		return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "ism %s lists %d roots, its retention window holds %d", entry.Id.String(), len(entry.Roots), want)
	}
	seen := make(map[string]struct{}, len(entry.Roots))
	for i, root := range entry.Roots {
		raw, err := DecodeHex(root)
		if err != nil {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "invalid tree root %q for ism %s: %v", root, entry.Id.String(), err)
		}
		if len(raw) != 32 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "invalid tree root length for ism %s: expected 32 bytes, got %d", entry.Id.String(), len(raw))
		}
		if _, dup := seen[string(raw)]; dup {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "tree root %s listed twice for ism %s", root, entry.Id.String())
		}
		seen[string(raw)] = struct{}{}
		if i == len(entry.Roots)-1 && !bytes.Equal(raw, ism.TreeRoot) {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "ism %s: newest listed root %s is not its tree root", entry.Id.String(), root)
		}
	}
	return nil
}
