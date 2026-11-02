package types

import (
	"math"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/internal/groth16"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// DefaultGenesis returns the default module genesis.
func DefaultGenesis() *GenesisState {
	return &GenesisState{}
}

// Validate performs basic genesis state validation.
func (gs GenesisState) Validate() error {
	// historySizes maps each ism to its effective root history size.
	historySizes := make(map[uint64]uint32, len(gs.Isms))
	for i := range gs.Isms {
		ism := gs.Isms[i]
		if ism.Id.IsZeroAddress() {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
		}

		internalID := ism.Id.GetInternalId()
		if _, exists := historySizes[internalID]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "duplicate ism id %s", ism.Id.String())
		}

		if err := ValidateRootHistorySize(ism.RootHistorySize); err != nil {
			return errorsmod.Wrapf(ErrInvalidRootHistorySize, "ism %s: %v", ism.Id.String(), err)
		}
		historySizes[internalID] = EffectiveRootHistorySize(ism.RootHistorySize)

		if ism.StateHeight < 0 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "negative state height for ism %s", ism.Id.String())
		}

		if len(ism.State) < MinStateBytes {
			return errorsmod.Wrapf(ErrInvalidTrustedState, "ism %s state must be at least %d bytes", ism.Id.String(), MinStateBytes)
		}

		if len(ism.State) > MaxStateBytes {
			return errorsmod.Wrapf(ErrInvalidTrustedState, "ism %s state must be no greater than %d bytes", ism.Id.String(), MaxStateBytes)
		}

		if len(ism.MerkleTreeAddress) != 32 {
			return errorsmod.Wrapf(ErrInvalidMerkleTreeAddress, "ism %s merkle tree address must be 32 bytes", ism.Id.String())
		}

		if err := ValidateGroth16Vkey(ism.Groth16Vkey); err != nil {
			return errorsmod.Wrapf(ErrInvalidVerifyingKey, "ism %s: %v", ism.Id.String(), err)
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

	treeStates := make(map[uint64]struct{}, len(gs.TreeStates))
	for i := range gs.TreeStates {
		entry := gs.TreeStates[i]
		if entry.Id.IsZeroAddress() {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
		}

		internalID := entry.Id.GetInternalId()
		if _, exists := historySizes[internalID]; !exists {
			return errorsmod.Wrapf(ErrIsmNotFound, "merkle tree state defined for unknown ism %s", entry.Id.String())
		}

		if _, exists := treeStates[internalID]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "duplicate merkle tree state entry for ism %s", entry.Id.String())
		}
		treeStates[internalID] = struct{}{}

		if len(entry.TreeState.Root) != 32 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "invalid merkle tree root length for ism %s: expected 32 bytes, got %d", entry.Id.String(), len(entry.TreeState.Root))
		}

		if entry.TreeState.Height < 0 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "negative merkle tree state height for ism %s", entry.Id.String())
		}
	}

	if err := validateRootHistory("state root", gs.StateRootHistory, historySizes); err != nil {
		return err
	}
	return validateRootHistory("merkle tree state", gs.TreeStateHistory, historySizes)
}

// validateRootHistory checks that each entry belongs to a known ism, has a unique sequence below MaxInt64 and a
// 32 byte root, and that no ism has more entries than its root history size.
func validateRootHistory(kind string, entries []GenesisRootHistoryEntry, historySizes map[uint64]uint32) error {
	type entryKey struct {
		ism      uint64
		sequence uint64
	}

	seen := make(map[entryKey]struct{}, len(entries))
	counts := make(map[uint64]uint32)
	for i := range entries {
		entry := entries[i]
		if entry.Id.IsZeroAddress() {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
		}

		internalID := entry.Id.GetInternalId()
		size, exists := historySizes[internalID]
		if !exists {
			return errorsmod.Wrapf(ErrIsmNotFound, "%s history defined for unknown ism %s", kind, entry.Id.String())
		}

		// Bound the sequence so the next push cannot wrap around.
		if entry.Sequence > math.MaxInt64 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "%s history sequence %d for ism %s exceeds %d", kind, entry.Sequence, entry.Id.String(), int64(math.MaxInt64))
		}

		key := entryKey{ism: internalID, sequence: entry.Sequence}
		if _, exists := seen[key]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "duplicate %s history sequence %d for ism %s", kind, entry.Sequence, entry.Id.String())
		}
		seen[key] = struct{}{}

		counts[internalID]++
		if counts[internalID] > size {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "%s history for ism %s exceeds root history size %d", kind, entry.Id.String(), size)
		}

		if len(entry.Entry.Root) != 32 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "invalid %s history root length for ism %s: expected 32 bytes, got %d", kind, entry.Id.String(), len(entry.Entry.Root))
		}

		if entry.Entry.Height < 0 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "negative %s history height for ism %s", kind, entry.Id.String())
		}
	}

	return nil
}
