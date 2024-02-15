/*
Package module contains application module patterns and associated "manager" functionality.
The module pattern has been broken down by:
  - independent module functionality (AppModuleBasic)
  - inter-dependent module genesis functionality (AppModuleGenesis)
  - inter-dependent module simulation functionality (AppModuleSimulation)
  - inter-dependent module full functionality (AppModule)

inter-dependent module functionality is module functionality which somehow
depends on other modules, typically through the module keeper.  Many of the
module keepers are dependent on each other, thus in order to access the full
set of module functionality we need to define all the keepers/params-store/keys
etc. This full set of advanced functionality is defined by the AppModule interface.

Independent module functions are separated to allow for the construction of the
basic application structures required early on in the application definition
and used to enable the definition of full module functionality later in the
process. This separation is necessary, however we still want to allow for a
high level pattern for modules to follow - for instance, such that we don't
have to manually register all of the codecs for all the modules. This basic
procedure as well as other basic patterns are handled through the use of
BasicManager.

Lastly the interface for genesis functionality (AppModuleGenesis) has been
separated out from full module functionality (AppModule) so that modules which
are only used for genesis can take advantage of the Module patterns without
needlessly defining many placeholder functions
*/
package module

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cosmos/cosmos-sdk/types/module"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Manager defines a module manager that provides the high level utility for managing and executing
// operations for a group of modules
type Manager struct {
	versionedModules   map[uint64]map[string]module.AppModule
	allModules         []module.AppModule
	firstVersion       uint64
	lastVersion        uint64
	OrderInitGenesis   []string
	OrderExportGenesis []string
	OrderBeginBlockers []string
	OrderEndBlockers   []string
	OrderMigrations    []string
}

type VersionedModule struct {
	module                 module.AppModule
	fromVersion, toVersion uint64
}

func NewVersionedModule(module module.AppModule, fromVersion, toVersion uint64) VersionedModule {
	return VersionedModule{
		module:      module,
		fromVersion: fromVersion,
		toVersion:   toVersion,
	}
}

// NewManager creates a new Manager object
func NewManager(modules ...VersionedModule) (*Manager, error) {
	moduleMap := make(map[uint64]map[string]module.AppModule)
	allModules := make([]module.AppModule, len(modules))
	modulesStr := make([]string, 0, len(modules))
	firstVersion, lastVersion := uint64(0), uint64(0)
	for _, module := range modules {
		if module.fromVersion == 0 {
			return nil, fmt.Errorf("v0 is not a valid version for module %s", module.module.Name())
		}
		if module.fromVersion > module.toVersion {
			return nil, fmt.Errorf("toVersion can not be less than fromVersion for module %s", module.module.Name())
		}
		for version := module.fromVersion; version <= module.toVersion; version++ {
			moduleMap[version][module.module.Name()] = module.module
		}
		allModules = append(allModules, module.module)
		modulesStr = append(modulesStr, module.module.Name())
		if firstVersion == 0 || module.fromVersion < firstVersion {
			firstVersion = module.fromVersion
		}
		if lastVersion == 0 || module.toVersion > lastVersion {
			lastVersion = module.toVersion
		}
	}

	return &Manager{
		versionedModules:   moduleMap,
		allModules:         allModules,
		firstVersion:       firstVersion,
		lastVersion:        lastVersion,
		OrderInitGenesis:   modulesStr,
		OrderExportGenesis: modulesStr,
		OrderBeginBlockers: modulesStr,
		OrderEndBlockers:   modulesStr,
	}, nil
}

// SetOrderInitGenesis sets the order of init genesis calls
func (m *Manager) SetOrderInitGenesis(moduleNames ...string) {
	m.assertNoForgottenModules("SetOrderInitGenesis", moduleNames)
	m.OrderInitGenesis = moduleNames
}

// SetOrderExportGenesis sets the order of export genesis calls
func (m *Manager) SetOrderExportGenesis(moduleNames ...string) {
	m.assertNoForgottenModules("SetOrderExportGenesis", moduleNames)
	m.OrderExportGenesis = moduleNames
}

// SetOrderBeginBlockers sets the order of set begin-blocker calls
func (m *Manager) SetOrderBeginBlockers(moduleNames ...string) {
	m.assertNoForgottenModules("SetOrderBeginBlockers", moduleNames)
	m.OrderBeginBlockers = moduleNames
}

// SetOrderEndBlockers sets the order of set end-blocker calls
func (m *Manager) SetOrderEndBlockers(moduleNames ...string) {
	m.assertNoForgottenModules("SetOrderEndBlockers", moduleNames)
	m.OrderEndBlockers = moduleNames
}

// SetOrderMigrations sets the order of migrations to be run. If not set
// then migrations will be run with an order defined in `DefaultMigrationsOrder`.
func (m *Manager) SetOrderMigrations(moduleNames ...string) {
	m.assertNoForgottenModules("SetOrderMigrations", moduleNames)
	m.OrderMigrations = moduleNames
}

// RegisterInvariants registers all module invariants
func (m *Manager) RegisterInvariants(ir sdk.InvariantRegistry) {
	for _, module := range m.allModules {
		module.RegisterInvariants(ir)
	}
}

// RegisterRoutes registers all module routes and module querier routes
func (m *Manager) RegisterRoutes(router sdk.Router, queryRouter sdk.QueryRouter, legacyQuerierCdc *codec.LegacyAmino) {
	for _, module := range m.allModules {
		if r := module.Route(); !r.Empty() {
			router.AddRoute(r)
		}
		if r := module.QuerierRoute(); r != "" {
			queryRouter.AddRoute(r, module.LegacyQuerierHandler(legacyQuerierCdc))
		}
	}
}

// RegisterServices registers all module services
func (m *Manager) RegisterServices(cfg module.Configurator) {
	for _, module := range m.allModules {
		module.RegisterServices(cfg)
	}
}

// InitGenesis performs init genesis functionality for modules. Exactly one
// module must return a non-empty validator set update to correctly initialize
// the chain.
func (m *Manager) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, genesisData map[string]json.RawMessage) abci.ResponseInitChain {
	var validatorUpdates []abci.ValidatorUpdate
	ctx.Logger().Info("initializing blockchain state from genesis.json")
	modules := m.versionedModules[m.firstVersion]
	for _, moduleName := range m.OrderInitGenesis {
		if genesisData[moduleName] == nil {
			continue
		}
		ctx.Logger().Debug("running initialization for module", "module", moduleName)

		moduleValUpdates := modules[moduleName].InitGenesis(ctx, cdc, genesisData[moduleName])

		// use these validator updates if provided, the module manager assumes
		// only one module will update the validator set
		if len(moduleValUpdates) > 0 {
			if len(validatorUpdates) > 0 {
				panic("validator InitGenesis updates already set by a previous module")
			}
			validatorUpdates = moduleValUpdates
		}
	}

	// a chain must initialize with a non-empty validator set
	if len(validatorUpdates) == 0 {
		panic(fmt.Sprintf("validator set is empty after InitGenesis, please ensure at least one validator is initialized with a delegation greater than or equal to the DefaultPowerReduction (%d)", sdk.DefaultPowerReduction))
	}

	return abci.ResponseInitChain{
		Validators: validatorUpdates,
	}
}

// ExportGenesis performs export genesis functionality for modules
func (m *Manager) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec, version uint64) map[string]json.RawMessage {
	genesisData := make(map[string]json.RawMessage)
	modules := m.versionedModules[version]
	for _, moduleName := range m.OrderExportGenesis {
		genesisData[moduleName] = modules[moduleName].ExportGenesis(ctx, cdc)
	}

	return genesisData
}

// assertNoForgottenModules checks that we didn't forget any modules in the
// SetOrder* functions.
func (m *Manager) assertNoForgottenModules(setOrderFnName string, moduleNames []string) {
	ms := make(map[string]bool)
	for _, m := range moduleNames {
		ms[m] = true
	}
	var missing []string
	for _, m := range m.allModules {
		if !ms[m.Name()] {
			missing = append(missing, m.Name())
		}
	}
	if len(missing) != 0 {
		panic(fmt.Sprintf(
			"%s: all modules must be defined when setting %s, missing: %v", setOrderFnName, setOrderFnName, missing))
	}
}

// MigrationHandler is the migration function that each module registers.
type MigrationHandler func(sdk.Context) error

// VersionMap is a map of moduleName -> version
type VersionMap map[string]uint64

// RunMigrations performs in-place store migrations for all modules. This
// function MUST be called when the state machine changes appVersion
func (m Manager) RunMigrations(ctx sdk.Context, cfg module.Configurator, fromVersion, toVersion uint64) error {
	c, ok := cfg.(configurator)
	if !ok {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidType, "expected %T, got %T", configurator{}, cfg)
	}
	modules := m.OrderMigrations
	if modules == nil {
		modules = DefaultMigrationsOrder(m.ModuleNames(toVersion))
	}
	currentVersionModules, exists := m.versionedModules[fromVersion]
	if !exists {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidVersion, "version %d not supported", fromVersion)
	}
	nextVersionModules, exists := m.versionedModules[toVersion]
	if !exists {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidVersion, "version %d not supported", toVersion)
	}

	for _, moduleName := range modules {
		_, currentModuleExists := currentVersionModules[moduleName]
		nextModule, nextModuleExists := nextVersionModules[moduleName]

		// if the module exists for both upgrades
		if currentModuleExists && nextModuleExists {
			err := c.runModuleMigrations(ctx, moduleName, fromVersion, toVersion)
			if err != nil {
				return err
			}
		} else if !currentModuleExists && nextModuleExists {
			ctx.Logger().Info(fmt.Sprintf("adding a new module: %s", moduleName))
			moduleValUpdates := nextModule.InitGenesis(ctx, c.cdc, nextModule.DefaultGenesis(c.cdc))
			// The module manager assumes only one module will update the
			// validator set, and it can't be a new module.
			if len(moduleValUpdates) > 0 {
				return sdkerrors.Wrapf(sdkerrors.ErrLogic, "validator InitGenesis update is already set by another module")
			}
		}
		// TODO: handle the case where a module is no longer supported (i.e. removed from the state machine)
	}

	return nil
}

// BeginBlock performs begin block functionality for all modules. It creates a
// child context with an event manager to aggregate events emitted from all
// modules.
func (m *Manager) BeginBlock(ctx sdk.Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock {
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	modules := m.versionedModules[ctx.BlockHeader().Version.App]
	for _, moduleName := range m.OrderBeginBlockers {
		module, ok := modules[moduleName].(module.BeginBlockAppModule)
		if ok {
			module.BeginBlock(ctx, req)
		}
	}

	return abci.ResponseBeginBlock{
		Events: ctx.EventManager().ABCIEvents(),
	}
}

// EndBlock performs end block functionality for all modules. It creates a
// child context with an event manager to aggregate events emitted from all
// modules.
func (m *Manager) EndBlock(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	validatorUpdates := []abci.ValidatorUpdate{}

	modules := m.versionedModules[ctx.BlockHeader().Version.App]
	for _, moduleName := range m.OrderEndBlockers {
		module, ok := modules[moduleName].(module.EndBlockAppModule)
		if !ok {
			continue
		}
		moduleValUpdates := module.EndBlock(ctx, req)

		// use these validator updates if provided, the module manager assumes
		// only one module will update the validator set
		if len(moduleValUpdates) > 0 {
			if len(validatorUpdates) > 0 {
				panic("validator EndBlock updates already set by a previous module")
			}

			validatorUpdates = moduleValUpdates
		}
	}

	return abci.ResponseEndBlock{
		ValidatorUpdates: validatorUpdates,
		Events:           ctx.EventManager().ABCIEvents(),
	}
}

// ModuleNames returns list of all module names, without any particular order.
func (m *Manager) ModuleNames(version uint64) []string {
	modules, ok := m.versionedModules[version]
	if !ok {
		return []string{}
	}

	ms := make([]string, len(modules))
	i := 0
	for m := range modules {
		ms[i] = m
		i++
	}
	return ms
}

// DefaultMigrationsOrder returns a default migrations order: ascending alphabetical by module name,
// except x/auth which will run last, see:
// https://github.com/cosmos/cosmos-sdk/issues/10591
func DefaultMigrationsOrder(modules []string) []string {
	const authName = "auth"
	out := make([]string, 0, len(modules))
	hasAuth := false
	for _, m := range modules {
		if m == authName {
			hasAuth = true
		} else {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	if hasAuth {
		out = append(out, authName)
	}
	return out
}
