package module

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
)

type VersionedModule struct {
	Module sdkmodule.AppModule
	// fromVersion and toVersion indicate the continuous range of app versions that the particular
	// module is part of. The range is inclusive. `fromVersion` should not be smaller than `toVersion`
	// 0 is not a valid app version
	FromVersion, ToVersion uint64
}

// MigrationHandler is the migration function that each module registers.
type MigrationHandler func(sdk.Context) error

// VersionMap is a map of moduleName -> version
type VersionMap map[string]uint64
