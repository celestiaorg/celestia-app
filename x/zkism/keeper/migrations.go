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

// MigrateClearSubmissions clears the proof submission flags so that the first merkle tree state submission is not
// blocked until the next state transition. Messages are now verified against a proven source chain merkle tree root,
// so pending messages become deliverable again once a merkle tree state is submitted. This requires the ISM's state
// membership program to output the new public values, so owners of existing ISMs must first point state_membership_vkey
// at the new program via UpdateVerifyingKeys.
//
// The per message id authorizations under prefix 1 are intentionally not deleted: nothing reads them anymore, and
// their number is attacker controlled, so clearing them would be unbounded work in the upgrade block.
func (m Migrator) MigrateClearSubmissions(ctx sdk.Context) error {
	return m.keeper.submissions.Clear(ctx, nil)
}
