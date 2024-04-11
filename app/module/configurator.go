package module

import (
	"fmt"

	pbgrpc "github.com/gogo/protobuf/grpc"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/module"
)

var _ module.Configurator = Configurator{}

// Configurator is a struct used at startup to register all the message and
// query servers for all modules. It allows the module to register any migrations from
// one consensus version of the module to the next. Finally it maps all the messages
// to the app versions that they are accepted in. This then gets used in the antehandler
// to prevent users from submitting messages that can not yet be executed.
type Configurator struct {
	fromVersion, toVersion uint64
	cdc                    codec.Codec
	msgServer              pbgrpc.Server
	queryServer            pbgrpc.Server
	// acceptedMsgs is a map from appVersion -> msgTypeURL -> struct{}.
	acceptedMessages map[uint64]map[string]struct{}
	// migrations is a map of moduleName -> fromVersion -> migration script handler.
	migrations map[string]map[uint64]module.MigrationHandler
}

// NewConfigurator returns a new Configurator instance.
func NewConfigurator(cdc codec.Codec, msgServer, queryServer pbgrpc.Server) Configurator {
	return Configurator{
		cdc:              cdc,
		msgServer:        msgServer,
		queryServer:      queryServer,
		migrations:       map[string]map[uint64]module.MigrationHandler{},
		acceptedMessages: map[uint64]map[string]struct{}{},
	}
}

func (c *Configurator) WithVersions(fromVersion, toVersion uint64) module.Configurator {
	c.fromVersion = fromVersion
	c.toVersion = toVersion
	return c
}

// MsgServer implements the Configurator.MsgServer method.
func (c Configurator) MsgServer() pbgrpc.Server {
	return &serverWrapper{
		addMessages: c.addMessages,
		msgServer:   c.msgServer,
	}
}

func (c Configurator) GetAcceptedMessages() map[uint64]map[string]struct{} {
	return c.acceptedMessages
}

// QueryServer implements the Configurator.QueryServer method
func (c Configurator) QueryServer() pbgrpc.Server {
	return c.queryServer
}

// RegisterMigration implements the Configurator.RegisterMigration method
func (c Configurator) RegisterMigration(moduleName string, fromVersion uint64, handler module.MigrationHandler) error {
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

func (c Configurator) addMessages(msgs []string) {
	for version := c.fromVersion; version <= c.toVersion; version++ {
		if _, exists := c.acceptedMessages[version]; !exists {
			c.acceptedMessages[version] = map[string]struct{}{}
		}
		for _, msg := range msgs {
			c.acceptedMessages[version][msg] = struct{}{}
		}
	}
}

// runModuleMigrations runs all in-place store migrations for one given module from a
// version to another version.
func (c Configurator) runModuleMigrations(ctx sdk.Context, moduleName string, fromVersion, toVersion uint64) error {
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
