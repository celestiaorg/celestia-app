package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	minfeetypes "github.com/celestiaorg/celestia-app/v4/x/minfee/types"
)

// Migrator is responsible for handling migrations related to the minfee module.
type Migrator struct {
	keeper *Keeper
}

// NewMigrator creates a new Migrator instance using the provided Keeper for handling migrations in the minfee module.
func NewMigrator(keeper *Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// MigrateParams handles the migration of minfee module parameters stored in the legacy subspace to the new parameter store.
// It validates the existing parameters and sets them in the updated format using the Keeper's parameter store.
func (m *Migrator) MigrateParams(ctx sdk.Context) error {
	var params minfeetypes.Params
	m.keeper.legacySubspace.GetParamSet(ctx, &params)
	if err := params.Validate(); err != nil {
		return err
	}
	m.keeper.SetParams(ctx, minfeetypes.NewParams(params.NetworkMinGasPrice))
	return nil
}
