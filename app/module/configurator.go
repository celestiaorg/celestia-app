package module

import (
	"fmt"

	"github.com/gogo/protobuf/grpc"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/module"
)

type configurator struct {
	cdc         codec.Codec
	msgServer   grpc.Server
	queryServer grpc.Server

	// migrations is a map of moduleName -> fromVersion -> migration script handler
	migrations map[string]map[uint64]module.MigrationHandler
}

// NewConfigurator returns a new Configurator instance
func NewConfigurator(cdc codec.Codec, msgServer grpc.Server, queryServer grpc.Server) module.Configurator {
	return configurator{
		cdc:         cdc,
		msgServer:   msgServer,
		queryServer: queryServer,
		migrations:  map[string]map[uint64]module.MigrationHandler{},
	}
}

var _ module.Configurator = configurator{}

// MsgServer implements the Configurator.MsgServer method
func (c configurator) MsgServer() grpc.Server {
	return c.msgServer
}

// QueryServer implements the Configurator.QueryServer method
func (c configurator) QueryServer() grpc.Server {
	return c.queryServer
}

// RegisterMigration implements the Configurator.RegisterMigration method
func (c configurator) RegisterMigration(moduleName string, fromVersion uint64, handler module.MigrationHandler) error {
	if fromVersion == 0 {
		return sdkerrors.ErrInvalidVersion.Wrap("module migration versions should start at 1")
	}

	if c.migrations[moduleName] == nil {
		c.migrations[moduleName] = map[uint64]module.MigrationHandler{}
	}

	if c.migrations[moduleName][fromVersion] != nil {
		return sdkerrors.ErrLogic.Wrapf("another migration for module %s and version %d already exists", moduleName, fromVersion)
	}

	c.migrations[moduleName][fromVersion] = handler

	return nil
}

// runModuleMigrations runs all in-place store migrations for one given module from a
// version to another version.
func (c configurator) runModuleMigrations(ctx sdk.Context, moduleName string, fromVersion, toVersion uint64) error {
	// No-op if toVersion is the initial version or if the version is unchanged.
	if toVersion <= 1 || fromVersion == toVersion {
		return nil
	}

	moduleMigrationsMap, found := c.migrations[moduleName]
	if !found {
		return sdkerrors.ErrNotFound.Wrapf("no migrations found for module %s", moduleName)
	}

	// Run in-place migrations for the module sequentially until toVersion.
	for i := fromVersion; i < toVersion; i++ {
		migrateFn, found := moduleMigrationsMap[i]
		if !found {
			// no migrations needed
			continue
		}
		ctx.Logger().Info(fmt.Sprintf("migrating module %s from version %d to version %d", moduleName, i, i+1))

		err := migrateFn(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}
