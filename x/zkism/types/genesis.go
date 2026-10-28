package types

import (
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
		if _, exists := isms[internalID]; !exists {
			return errorsmod.Wrapf(ErrIsmNotFound, "merkle tree state defined for unknown ism %s", entry.Id.String())
		}

		if _, exists := treeStates[internalID]; exists {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "duplicate merkle tree state entry for ism %s", entry.Id.String())
		}
		treeStates[internalID] = struct{}{}

		if len(entry.TreeState.Root) != 32 {
			return errorsmod.Wrapf(sdkerrors.ErrAppConfig, "invalid merkle tree root length for ism %s: expected 32 bytes, got %d", entry.Id.String(), len(entry.TreeState.Root))
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
