package module

import (
	"encoding/json"
	"fmt"
	"sort"

	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Manager defines a module manager that provides the high level utility for managing and executing
// operations for a group of modules. This implemention maps the state machine version to different
// versions of the module. It also provides a way to run migrations between different versions of a
// module.
type Manager struct {
	versionedModules map[uint64]map[string]sdkmodule.AppModule
	// this is a mapping of module name to module consensus version to the range of
	// app versions this particular module operates over. The first element in the
	// array represent the fromVersion and the last the toVersion (this is inclusive)
	uniqueModuleVersions map[string]map[uint64][2]uint64
	allModules           []sdkmodule.AppModule
	firstVersion         uint64
	lastVersion          uint64
	OrderInitGenesis     []string
	OrderExportGenesis   []string
	OrderBeginBlockers   []string
	OrderEndBlockers     []string
	OrderMigrations      []string
}

type VersionedModule struct {
	Module sdkmodule.AppModule
	// fromVersion and toVersion indicate the continuous range of app versions that the particular
	// module is part of. The range is inclusive. `fromVersion` should not be smaller than `toVersion`
	// 0 is not a valid app version
	FromVersion, ToVersion uint64
}

// NewManager creates a new Manager object
func NewManager(modules []VersionedModule) (*Manager, error) {
	moduleMap := make(map[uint64]map[string]sdkmodule.AppModule)
	allModules := make([]sdkmodule.AppModule, len(modules))
	modulesStr := make([]string, 0, len(modules))
	uniqueModuleVersions := make(map[string]map[uint64][2]uint64)
	// firstVersion and lastVersion are quicker ways of working out the range of
	// versions the state machine supports
	firstVersion, lastVersion := uint64(0), uint64(0)
	for idx, module := range modules {
		name := module.Module.Name()
		moduleVersion := module.Module.ConsensusVersion()
		if module.FromVersion == 0 {
			return nil, sdkerrors.ErrInvalidVersion.Wrapf("v0 is not a valid version for module %s", module.Module.Name())
		}
		if module.FromVersion > module.ToVersion {
			return nil, sdkerrors.ErrLogic.Wrapf("FromVersion cannot be greater than ToVersion for module %s", module.Module.Name())
		}
		for version := module.FromVersion; version <= module.ToVersion; version++ {
			if moduleMap[version] == nil {
				moduleMap[version] = make(map[string]sdkmodule.AppModule)
			}
			if _, exists := moduleMap[version][name]; exists {
				return nil, sdkerrors.ErrLogic.Wrapf("Two different modules with domain %s are registered with the same version %d", name, version)
			}
			moduleMap[version][module.Module.Name()] = module.Module
		}
		allModules[idx] = module.Module
		modulesStr = append(modulesStr, name)
		if _, exists := uniqueModuleVersions[name]; !exists {
			uniqueModuleVersions[name] = make(map[uint64][2]uint64)
		}
		uniqueModuleVersions[name][moduleVersion] = [2]uint64{module.FromVersion, module.ToVersion}
		if firstVersion == 0 || module.FromVersion < firstVersion {
			firstVersion = module.FromVersion
		}
		if lastVersion == 0 || module.ToVersion > lastVersion {
			lastVersion = module.ToVersion
		}
	}

	m := &Manager{
		versionedModules:     moduleMap,
		uniqueModuleVersions: uniqueModuleVersions,
		allModules:           allModules,
		firstVersion:         firstVersion,
		lastVersion:          lastVersion,
		OrderInitGenesis:     modulesStr,
		OrderExportGenesis:   modulesStr,
		OrderBeginBlockers:   modulesStr,
		OrderEndBlockers:     modulesStr,
	}
	if err := m.checkUpgradeSchedule(); err != nil {
		return nil, err
	}
	return m, nil
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

// SetOrderBeginBlockers sets the order of begin-blocker calls
func (m *Manager) SetOrderBeginBlockers(moduleNames ...string) {
	m.assertNoForgottenModules("SetOrderBeginBlockers", moduleNames)
	m.OrderBeginBlockers = moduleNames
}

// SetOrderEndBlockers sets the order of end-blocker calls
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

// RegisterServices registers all module services
func (m *Manager) RegisterServices(cfg VersionedConfigurator) {
	for _, module := range m.allModules {
		fromVersion, toVersion := m.getAppVersionsForModule(module.Name(), module.ConsensusVersion())
		module.RegisterServices(cfg.WithVersions(fromVersion, toVersion))
	}
}

func (m *Manager) getAppVersionsForModule(moduleName string, moduleVersion uint64) (uint64, uint64) {
	return m.uniqueModuleVersions[moduleName][moduleVersion][0], m.uniqueModuleVersions[moduleName][moduleVersion][1]
}

// InitGenesis performs init genesis functionality for modules. Exactly one
// module must return a non-empty validator set update to correctly initialize
// the chain.
func (m *Manager) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, genesisData map[string]json.RawMessage, appVersion uint64) abci.ResponseInitChain {
	var validatorUpdates []abci.ValidatorUpdate
	ctx.Logger().Info("initializing blockchain state from genesis.json")
	modules, versionSupported := m.versionedModules[appVersion]
	if !versionSupported {
		panic(fmt.Sprintf("version %d not supported", appVersion))
	}
	for _, moduleName := range m.OrderInitGenesis {
		if genesisData[moduleName] == nil {
			continue
		}
		if modules[moduleName] == nil {
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
		if _, ok := ms[m.Name()]; !ok {
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
// function MUST be called when the state machine changes appVersion.
func (m Manager) RunMigrations(ctx sdk.Context, cfg sdkmodule.Configurator, fromVersion, toVersion uint64) error {
	c, ok := cfg.(VersionedConfigurator)
	if !ok {
		return sdkerrors.ErrInvalidType.Wrapf("expected %T, got %T", VersionedConfigurator{}, cfg)
	}
	modules := m.OrderMigrations
	if modules == nil {
		modules = DefaultMigrationsOrder(m.ModuleNames(toVersion))
	}
	currentVersionModules, exists := m.versionedModules[fromVersion]
	if !exists {
		return sdkerrors.ErrInvalidVersion.Wrapf("fromVersion %d not supported", fromVersion)
	}
	nextVersionModules, exists := m.versionedModules[toVersion]
	if !exists {
		return sdkerrors.ErrInvalidVersion.Wrapf("toVersion %d not supported", toVersion)
	}

	for _, moduleName := range modules {
		currentModule, currentModuleExists := currentVersionModules[moduleName]
		nextModule, nextModuleExists := nextVersionModules[moduleName]

		if isModuleExisting(currentModuleExists, nextModuleExists) {
			// by using consensus version instead of app version we support the SDK's legacy method
			// of migrating modules which were made of several versions and consisted of a mapping of
			// app version to module version. Now, using go.mod, each module will have only a single
			// consensus version and each breaking upgrade will result in a new module and a new consensus
			// version.
			fromModuleVersion := currentModule.ConsensusVersion()
			toModuleVersion := nextModule.ConsensusVersion()
			err := c.runModuleMigrations(ctx, moduleName, fromModuleVersion, toModuleVersion)
			if err != nil {
				return err
			}
		} else if isModuleNew(currentModuleExists, nextModuleExists) {
			ctx.Logger().Info(fmt.Sprintf("adding a new module: %s", moduleName))
			moduleValUpdates := nextModule.InitGenesis(ctx, c.cdc, nextModule.DefaultGenesis(c.cdc))
			// The module manager assumes only one module will update the
			// validator set, and it can't be a new module.
			if len(moduleValUpdates) > 0 {
				return sdkerrors.ErrLogic.Wrap("validator InitGenesis update is already set by another module")
			}
		} else if isModuleRemoved(currentModuleExists, nextModuleExists) {
			ctx.Logger().Info(fmt.Sprintf("removing an existing module: %s", moduleName))
			fromModuleVersion := currentModule.ConsensusVersion()
			toModuleVersion := nextModule.ConsensusVersion()
			err := c.runModuleMigrations(ctx, moduleName, fromModuleVersion, toModuleVersion)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// BeginBlock performs begin block functionality for all modules. It creates a
// child context with an event manager to aggregate events emitted from all
// modules.
func (m *Manager) BeginBlock(ctx sdk.Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock {
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	modules := m.versionedModules[ctx.BlockHeader().Version.App]
	if modules == nil {
		panic(fmt.Sprintf("no modules for version %d", ctx.BlockHeader().Version.App))
	}
	for _, moduleName := range m.OrderBeginBlockers {
		module, ok := modules[moduleName].(sdkmodule.BeginBlockAppModule)
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
	if modules == nil {
		panic(fmt.Sprintf("no modules for version %d", ctx.BlockHeader().Version.App))
	}
	for _, moduleName := range m.OrderEndBlockers {
		module, ok := modules[moduleName].(sdkmodule.EndBlockAppModule)
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

func (m *Manager) SupportedVersions() []uint64 {
	output := make([]uint64, 0, m.lastVersion-m.firstVersion+1)
	for version := m.firstVersion; version <= m.lastVersion; version++ {
		if _, ok := m.versionedModules[version]; ok {
			output = append(output, version)
		}
	}
	return output
}

// checkUgradeSchedule performs a dry run of all the upgrades in all versions and asserts that the consensus version
// for a module domain i.e. auth, always increments for each module that uses the auth domain name
func (m *Manager) checkUpgradeSchedule() error {
	if m.firstVersion == m.lastVersion {
		// there are no upgrades to check
		return nil
	}
	for _, moduleName := range m.OrderInitGenesis {
		lastConsensusVersion := uint64(0)
		for appVersion := m.firstVersion; appVersion <= m.lastVersion; appVersion++ {
			module, exists := m.versionedModules[appVersion][moduleName]
			if !exists {
				continue
			}
			moduleVersion := module.ConsensusVersion()
			if moduleVersion < lastConsensusVersion {
				return fmt.Errorf("error: module %s in appVersion %d goes from moduleVersion %d to %d", moduleName, appVersion, lastConsensusVersion, moduleVersion)
			}
			lastConsensusVersion = moduleVersion
		}
	}
	return nil
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

func isModuleExisting(currentModuleExists, nextModuleExists bool) bool {
	return currentModuleExists && nextModuleExists
}

func isModuleNew(currentModuleExists, nextModuleExists bool) bool {
	return !currentModuleExists && nextModuleExists
}

func isModuleRemoved(currentModuleExists, nextModuleExists bool) bool {
	return currentModuleExists && !nextModuleExists
}
