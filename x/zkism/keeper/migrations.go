package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Migrator is a struct for handling in-place store migrations.
type Migrator struct {
	keeper *Keeper
}

// NewMigrator returns a new Migrator.
func NewMigrator(keeper *Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate1to2 clears the per message id authorizations and proof submission flags. Messages are now verified
// against a proven source chain merkle tree root, so pending messages become deliverable again once a merkle
// tree state is submitted. This requires the ISM's state membership program to output the new public values, so
// owners of existing ISMs must first point state_membership_vkey at the new program via UpdateVerifyingKeys.
// Submission flags are cleared so that the first submission is not blocked until the next state transition.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	if err := m.keeper.legacyMessages.Clear(ctx, nil); err != nil {
		return err
	}

	return m.keeper.submissions.Clear(ctx, nil)
}
