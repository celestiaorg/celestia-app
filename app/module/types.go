package module

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
)

type VersionedModule struct {
	Module sdkmodule.AppModule
	// FromVersion and ToVersion indicate the continuous range of app versions
	// that this particular module is part of. The range is inclusive.
	// FromVersion should not be smaller than ToVersion. 0 is not a valid app
	// version.
	FromVersion, ToVersion uint64
}

// MigrationHandler is the migration function that each module registers.
type MigrationHandler func(sdk.Context) error
