package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

// Migrator is responsible for handling migrations related to the blob module.
type Migrator struct {
	keeper Keeper
}

// NewMigrator creates a new Migrator instance using the provided Keeper for handling migrations in the blob module.
func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// MigrateParams handles the migration of blob module parameters stored in the legacy subspace to the new parameter store.
// It validates the existing parameters and sets them in the updated format using the Keeper's parameter store.
func (m *Migrator) MigrateParams(ctx sdk.Context) error {
	var params blobtypes.Params
	m.keeper.legacySubspace.GetParamSet(ctx, &params)

	if err := params.Validate(); err != nil {
		return err
	}

	m.keeper.SetParams(ctx, params)
	return nil
}
