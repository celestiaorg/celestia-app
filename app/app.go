package app

import (
	"fmt"
	"io"
	"slices"

	"github.com/celestiaorg/celestia-app/v3/app/ante"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	celestiatx "github.com/celestiaorg/celestia-app/v3/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v3/app/module"
	"github.com/celestiaorg/celestia-app/v3/app/posthandler"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	appv1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	appv2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	appv3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/celestiaorg/celestia-app/v3/pkg/proof"
	blobkeeper "github.com/celestiaorg/celestia-app/v3/x/blob/keeper"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	blobstreamkeeper "github.com/celestiaorg/celestia-app/v3/x/blobstream/keeper"
	blobstreamtypes "github.com/celestiaorg/celestia-app/v3/x/blobstream/types"
	"github.com/celestiaorg/celestia-app/v3/x/minfee"
	mintkeeper "github.com/celestiaorg/celestia-app/v3/x/mint/keeper"
	minttypes "github.com/celestiaorg/celestia-app/v3/x/mint/types"
	"github.com/celestiaorg/celestia-app/v3/x/paramfilter"
	"github.com/celestiaorg/celestia-app/v3/x/signal"
	signaltypes "github.com/celestiaorg/celestia-app/v3/x/signal/types"
	"github.com/celestiaorg/celestia-app/v3/x/tokenfilter"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	capabilitykeeper "github.com/cosmos/cosmos-sdk/x/capability/keeper"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	crisiskeeper "github.com/cosmos/cosmos-sdk/x/crisis/keeper"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	evidencekeeper "github.com/cosmos/cosmos-sdk/x/evidence/keeper"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1beta2 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	oldgovtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	paramproposal "github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v6/packetforward"
	packetforwardkeeper "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v6/packetforward/keeper"
	packetforwardtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v6/packetforward/types"
	icahost "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host"
	icahostkeeper "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/keeper"
	icahosttypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/types"
	"github.com/cosmos/ibc-go/v6/modules/apps/transfer"
	ibctransferkeeper "github.com/cosmos/ibc-go/v6/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v6/modules/apps/transfer/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	ibcporttypes "github.com/cosmos/ibc-go/v6/modules/core/05-port/types"
	ibchost "github.com/cosmos/ibc-go/v6/modules/core/24-host"
	ibckeeper "github.com/cosmos/ibc-go/v6/modules/core/keeper"
	ibctesting "github.com/cosmos/ibc-go/v6/testing"
	ibctestingtypes "github.com/cosmos/ibc-go/v6/testing/types"
	"github.com/spf13/cast"
	abci "github.com/tendermint/tendermint/abci/types"
	tmjson "github.com/tendermint/tendermint/libs/json"
	"github.com/tendermint/tendermint/libs/log"
	tmos "github.com/tendermint/tendermint/libs/os"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

// maccPerms is short for module account permissions. It is a map from module
// account name to a list of permissions for that module account.
var maccPerms = map[string][]string{
	authtypes.FeeCollectorName:     nil,
	distrtypes.ModuleName:          nil,
	govtypes.ModuleName:            {authtypes.Burner},
	minttypes.ModuleName:           {authtypes.Minter},
	stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
	stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
	ibctransfertypes.ModuleName:    {authtypes.Minter, authtypes.Burner},
	icatypes.ModuleName:            nil,
}

const (
	v1                    = appv1.Version
	v2                    = appv2.Version
	v3                    = appv3.Version
	DefaultInitialVersion = v1
)

var (
	_ servertypes.Application = (*App)(nil)
	_ ibctesting.TestingApp   = (*App)(nil)
)

// App extends an ABCI application, but with most of its parameters exported.
// They are exported for convenience in creating helper functions, as object
// capabilities aren't needed for testing.
type App struct {
	*baseapp.BaseApp

	legacyAmino       *codec.LegacyAmino
	appCodec          codec.Codec
	interfaceRegistry types.InterfaceRegistry
	txConfig          client.TxConfig

	invCheckPeriod uint

	// keys to access the substores
	keyVersions map[uint64][]string
	keys        map[string]*storetypes.KVStoreKey
	tkeys       map[string]*storetypes.TransientStoreKey
	memKeys     map[string]*storetypes.MemoryStoreKey

	// keepers
	AccountKeeper       authkeeper.AccountKeeper
	BankKeeper          bankkeeper.Keeper
	AuthzKeeper         authzkeeper.Keeper
	CapabilityKeeper    *capabilitykeeper.Keeper
	StakingKeeper       stakingkeeper.Keeper
	SlashingKeeper      slashingkeeper.Keeper
	MintKeeper          mintkeeper.Keeper
	DistrKeeper         distrkeeper.Keeper
	GovKeeper           govkeeper.Keeper
	CrisisKeeper        crisiskeeper.Keeper
	UpgradeKeeper       upgradekeeper.Keeper // This is included purely for the IBC Keeper. It is not used for upgrading
	SignalKeeper        signal.Keeper
	ParamsKeeper        paramskeeper.Keeper
	IBCKeeper           *ibckeeper.Keeper // IBCKeeper must be a pointer in the app, so we can SetRouter on it correctly
	EvidenceKeeper      evidencekeeper.Keeper
	TransferKeeper      ibctransferkeeper.Keeper
	FeeGrantKeeper      feegrantkeeper.Keeper
	ICAHostKeeper       icahostkeeper.Keeper
	PacketForwardKeeper *packetforwardkeeper.Keeper
	BlobKeeper          blobkeeper.Keeper
	BlobstreamKeeper    blobstreamkeeper.Keeper

	ScopedIBCKeeper      capabilitykeeper.ScopedKeeper // This keeper is public for test purposes
	ScopedTransferKeeper capabilitykeeper.ScopedKeeper // This keeper is public for test purposes
	ScopedICAHostKeeper  capabilitykeeper.ScopedKeeper // This keeper is public for test purposes

	manager      *module.Manager
	configurator module.Configurator
	// upgradeHeightV2 is used as a coordination mechanism for the height-based
	// upgrade from v1 to v2.
	upgradeHeightV2 int64
	// MsgGateKeeper is used to define which messages are accepted for a given
	// app version.
	MsgGateKeeper *ante.MsgVersioningGateKeeper
}

// New returns a reference to an uninitialized app. Callers must subsequently
// call app.Info or app.InitChain to initialize the baseapp.
//
// NOTE: upgradeHeightV2 refers specifically to the height that a node will
// upgrade from v1 to v2. It will be deprecated in v3 in place for a dynamically
// signalling scheme
func New(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	invCheckPeriod uint,
	encodingConfig encoding.Config,
	upgradeHeightV2 int64,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *App {
	appCodec := encodingConfig.Codec
	interfaceRegistry := encodingConfig.InterfaceRegistry

	baseApp := baseapp.NewBaseApp(Name, logger, db, encodingConfig.TxConfig.TxDecoder(), baseAppOptions...)
	baseApp.SetCommitMultiStoreTracer(traceStore)
	baseApp.SetVersion(version.Version)
	baseApp.SetInterfaceRegistry(interfaceRegistry)

	keys := sdk.NewKVStoreKeys(allStoreKeys()...)
	tkeys := sdk.NewTransientStoreKeys(paramstypes.TStoreKey)
	memKeys := sdk.NewMemoryStoreKeys(capabilitytypes.MemStoreKey)

	app := &App{
		BaseApp:           baseApp,
		appCodec:          appCodec,
		interfaceRegistry: interfaceRegistry,
		txConfig:          encodingConfig.TxConfig,
		invCheckPeriod:    invCheckPeriod,
		keyVersions:       versionedStoreKeys(),
		keys:              keys,
		tkeys:             tkeys,
		memKeys:           memKeys,
		upgradeHeightV2:   upgradeHeightV2,
	}

	app.ParamsKeeper = initParamsKeeper(appCodec, encodingConfig.Amino, keys[paramstypes.StoreKey], tkeys[paramstypes.TStoreKey])

	baseApp.SetParamStore(app.ParamsKeeper.Subspace(baseapp.Paramspace).WithKeyTable(paramstypes.ConsensusParamsKeyTable()))

	app.CapabilityKeeper = capabilitykeeper.NewKeeper(appCodec, keys[capabilitytypes.StoreKey], memKeys[capabilitytypes.MemStoreKey])

	app.ScopedIBCKeeper = app.CapabilityKeeper.ScopeToModule(ibchost.ModuleName)
	app.ScopedTransferKeeper = app.CapabilityKeeper.ScopeToModule(ibctransfertypes.ModuleName)
	app.ScopedICAHostKeeper = app.CapabilityKeeper.ScopeToModule(icahosttypes.SubModuleName)

	app.AccountKeeper = authkeeper.NewAccountKeeper(
		appCodec, keys[authtypes.StoreKey], app.GetSubspace(authtypes.ModuleName), authtypes.ProtoBaseAccount, maccPerms, sdk.GetConfig().GetBech32AccountAddrPrefix(),
	)
	app.BankKeeper = bankkeeper.NewBaseKeeper(
		appCodec, keys[banktypes.StoreKey], app.AccountKeeper, app.GetSubspace(banktypes.ModuleName), app.ModuleAccountAddrs(),
	)
	app.AuthzKeeper = authzkeeper.NewKeeper(
		keys[authzkeeper.StoreKey], appCodec, baseApp.MsgServiceRouter(), app.AccountKeeper,
	)
	stakingKeeper := stakingkeeper.NewKeeper(
		appCodec, keys[stakingtypes.StoreKey], app.AccountKeeper, app.BankKeeper, app.GetSubspace(stakingtypes.ModuleName),
	)
	app.MintKeeper = mintkeeper.NewKeeper(
		appCodec,
		keys[minttypes.StoreKey],
		&stakingKeeper,
		app.AccountKeeper,
		app.BankKeeper,
		authtypes.FeeCollectorName,
	)
	app.DistrKeeper = distrkeeper.NewKeeper(
		appCodec, keys[distrtypes.StoreKey], app.GetSubspace(distrtypes.ModuleName), app.AccountKeeper, app.BankKeeper,
		&stakingKeeper, authtypes.FeeCollectorName,
	)
	app.SlashingKeeper = slashingkeeper.NewKeeper(
		appCodec, keys[slashingtypes.StoreKey], &stakingKeeper, app.GetSubspace(slashingtypes.ModuleName),
	)
	app.CrisisKeeper = crisiskeeper.NewKeeper(
		app.GetSubspace(crisistypes.ModuleName), invCheckPeriod, app.BankKeeper, authtypes.FeeCollectorName,
	)

	app.FeeGrantKeeper = feegrantkeeper.NewKeeper(appCodec, keys[feegrant.StoreKey], app.AccountKeeper)
	// The upgrade keeper is intialised solely for the ibc keeper which depends on it to know what the next validator hash is for after the
	// upgrade. This keeper is not used for the actual upgrades but merely for compatibility reasons. Ideally IBC has their own upgrade module
	// for performing IBC based upgrades. Note, as we use rolling upgrades, IBC technically never needs this functionality.
	app.UpgradeKeeper = upgradekeeper.NewKeeper(nil, keys[upgradetypes.StoreKey], appCodec, "", app.BaseApp, authtypes.NewModuleAddress(govtypes.ModuleName).String())

	app.BlobstreamKeeper = *blobstreamkeeper.NewKeeper(
		appCodec,
		keys[blobstreamtypes.StoreKey],
		app.GetSubspace(blobstreamtypes.ModuleName),
		&stakingKeeper,
	)

	// Register the staking hooks. NOTE: stakingKeeper is passed by reference
	// above so that it will contain these hooks.
	app.StakingKeeper = *stakingKeeper.SetHooks(
		stakingtypes.NewMultiStakingHooks(
			app.DistrKeeper.Hooks(),
			app.SlashingKeeper.Hooks(),
			app.BlobstreamKeeper.Hooks(),
		),
	)

	app.SignalKeeper = signal.NewKeeper(appCodec, keys[signaltypes.StoreKey], app.StakingKeeper)

	app.IBCKeeper = ibckeeper.NewKeeper(
		appCodec,
		keys[ibchost.StoreKey],
		app.GetSubspace(ibchost.ModuleName),
		app.StakingKeeper,
		app.UpgradeKeeper,
		app.ScopedIBCKeeper,
	)

	app.ICAHostKeeper = icahostkeeper.NewKeeper(
		appCodec,
		keys[icahosttypes.StoreKey],
		app.GetSubspace(icahosttypes.SubModuleName),
		app.IBCKeeper.ChannelKeeper, // ICS4Wrapper
		app.IBCKeeper.ChannelKeeper,
		&app.IBCKeeper.PortKeeper,
		app.AccountKeeper,
		app.ScopedICAHostKeeper,
		app.MsgServiceRouter(),
	)

	paramBlockList := paramfilter.NewParamBlockList(app.BlockedParams()...)

	// Register the proposal types.
	govRouter := oldgovtypes.NewRouter()
	govRouter.AddRoute(paramproposal.RouterKey, paramBlockList.GovHandler(app.ParamsKeeper)).
		AddRoute(distrtypes.RouterKey, distr.NewCommunityPoolSpendProposalHandler(app.DistrKeeper)).
		AddRoute(ibcclienttypes.RouterKey, NewClientProposalHandler(app.IBCKeeper.ClientKeeper))

	// Create Transfer Keepers.
	tokenFilterKeeper := tokenfilter.NewKeeper(app.IBCKeeper.ChannelKeeper)

	app.PacketForwardKeeper = packetforwardkeeper.NewKeeper(
		appCodec,
		keys[packetforwardtypes.StoreKey],
		app.GetSubspace(packetforwardtypes.ModuleName),
		app.TransferKeeper, // will be zero-value here, reference is set later on with SetTransferKeeper.
		app.IBCKeeper.ChannelKeeper,
		app.DistrKeeper,
		app.BankKeeper,
		tokenFilterKeeper,
	)

	app.TransferKeeper = ibctransferkeeper.NewKeeper(
		appCodec, keys[ibctransfertypes.StoreKey], app.GetSubspace(ibctransfertypes.ModuleName),
		app.PacketForwardKeeper, app.IBCKeeper.ChannelKeeper, &app.IBCKeeper.PortKeeper,
		app.AccountKeeper, app.BankKeeper, app.ScopedTransferKeeper,
	)
	// Transfer stack contains (from top to bottom):
	// - Token Filter
	// - Packet Forwarding Middleware
	// - Transfer
	var transferStack ibcporttypes.IBCModule
	transferStack = transfer.NewIBCModule(app.TransferKeeper)
	packetForwardMiddleware := packetforward.NewIBCMiddleware(
		transferStack,
		app.PacketForwardKeeper,
		0, // retries on timeout
		packetforwardkeeper.DefaultForwardTransferPacketTimeoutTimestamp, // forward timeout
		packetforwardkeeper.DefaultRefundTransferPacketTimeoutTimestamp,  // refund timeout
	)
	// PacketForwardMiddleware is used only for version >= 2.
	transferStack = module.NewVersionedIBCModule(packetForwardMiddleware, transferStack, v2, v3)
	// Token filter wraps packet forward middleware and is thus the first module in the transfer stack.
	tokenFilterMiddelware := tokenfilter.NewIBCMiddleware(transferStack)
	transferStack = module.NewVersionedIBCModule(tokenFilterMiddelware, transferStack, v1, v3)

	app.EvidenceKeeper = *evidencekeeper.NewKeeper(
		appCodec,
		keys[evidencetypes.StoreKey],
		&app.StakingKeeper,
		app.SlashingKeeper,
	)

	app.GovKeeper = govkeeper.NewKeeper(
		appCodec,
		keys[govtypes.StoreKey],
		app.GetSubspace(govtypes.ModuleName),
		app.AccountKeeper,
		app.BankKeeper,
		&stakingKeeper,
		govRouter,
		baseApp.MsgServiceRouter(),
		govtypes.DefaultConfig(),
	)

	app.BlobKeeper = *blobkeeper.NewKeeper(
		appCodec,
		app.GetSubspace(blobtypes.ModuleName),
	)

	app.PacketForwardKeeper.SetTransferKeeper(app.TransferKeeper)
	ibcRouter := ibcporttypes.NewRouter()                                                   // Create static IBC router
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transferStack)                          // Add transfer route
	ibcRouter.AddRoute(icahosttypes.SubModuleName, icahost.NewIBCModule(app.ICAHostKeeper)) // Add ICA route
	app.IBCKeeper.SetRouter(ibcRouter)

	/****  Module Options ****/

	// NOTE: we may consider parsing `appOpts` inside module constructors. For the moment
	// we prefer to be more strict in what arguments the modules expect.
	skipGenesisInvariants := cast.ToBool(appOpts.Get(crisis.FlagSkipGenesisInvariants))

	// NOTE: Modules can't be modified or else must be passed by reference to the module manager
	err := app.setupModuleManager(skipGenesisInvariants)
	if err != nil {
		panic(err)
	}

	// order begin block, end block and init genesis
	app.setModuleOrder()

	app.QueryRouter().AddRoute(proof.TxInclusionQueryPath, proof.QueryTxInclusionProof)
	app.QueryRouter().AddRoute(proof.ShareInclusionQueryPath, proof.QueryShareInclusionProof)

	app.manager.RegisterInvariants(&app.CrisisKeeper)
	app.configurator = module.NewConfigurator(app.appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	app.manager.RegisterServices(app.configurator)

	// extract the accepted message list from the configurator and create a gatekeeper
	// which will be used both as the antehandler and as part of the circuit breaker in
	// the msg service router
	app.MsgGateKeeper = ante.NewMsgVersioningGateKeeper(app.configurator.GetAcceptedMessages())
	app.MsgServiceRouter().SetCircuit(app.MsgGateKeeper)

	// Initialize the KV stores for the base modules (e.g. params). The base modules will be included in every app version.
	app.MountKVStores(app.baseKeys())
	app.MountTransientStores(tkeys)
	app.MountMemoryStores(memKeys)

	app.SetInitChainer(app.InitChainer)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
	app.SetAnteHandler(ante.NewAnteHandler(
		app.AccountKeeper,
		app.BankKeeper,
		app.BlobKeeper,
		app.FeeGrantKeeper,
		encodingConfig.TxConfig.SignModeHandler(),
		ante.DefaultSigVerificationGasConsumer,
		app.IBCKeeper,
		app.ParamsKeeper,
		app.MsgGateKeeper,
	))
	app.SetPostHandler(posthandler.New())

	app.SetMigrateStoreFn(app.migrateCommitStore)
	app.SetMigrateModuleFn(app.migrateModules)

	// assert that keys are present for all supported versions
	app.assertAllKeysArePresent()

	// we don't seal the store until the app version has been initialised
	// this will just initialize the base keys (i.e. the param store)
	if err := app.CommitMultiStore().LoadLatestVersion(); err != nil {
		tmos.Exit(err.Error())
	}

	return app
}

// Name returns the name of the App
func (app *App) Name() string { return app.BaseApp.Name() }

// BeginBlocker application updates every begin block
func (app *App) BeginBlocker(ctx sdk.Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock {
	if req.Header.Height == app.upgradeHeightV2 {
		app.BaseApp.Logger().Info("upgraded from app version 1 to 2")
	}
	return app.manager.BeginBlock(ctx, req)
}

// EndBlocker executes application updates at the end of every block.
func (app *App) EndBlocker(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
	res := app.manager.EndBlock(ctx, req)
	currentVersion := app.AppVersion()
	// For v1 only we upgrade using an agreed upon height known ahead of time
	if currentVersion == v1 {
		// check that we are at the height before the upgrade
		if req.Height == app.upgradeHeightV2-1 {
			app.BaseApp.Logger().Info(fmt.Sprintf("upgrading from app version %v to 2", currentVersion))
			app.SetInitialAppVersionInConsensusParams(ctx, v2)
			app.SetAppVersion(ctx, v2)

			// The blobstream module was disabled in v2 so the following line
			// removes the params subspace for blobstream.
			if err := app.ParamsKeeper.DeleteSubspace(blobstreamtypes.ModuleName); err != nil {
				panic(err)
			}
		}
		// from v2 to v3 and onwards we use a signalling mechanism
	} else if shouldUpgrade, newVersion := app.SignalKeeper.ShouldUpgrade(ctx); shouldUpgrade {
		// Version changes must be increasing. Downgrades are not permitted
		if newVersion > currentVersion {
			app.SetAppVersion(ctx, newVersion)
			app.SignalKeeper.ResetTally(ctx)
		}
	}
	// Update timeouts based on the current version.
	// TODO: We check app.AppVersion(), which may differ from currentVersion,
	// as we want to set the timeouts based on the app version
	// that will be used in the next block.
	v := app.AppVersion()
	res.TimeoutCommit = appconsts.GetTimeoutCommit(v)
	res.TimeoutPropose = appconsts.GetTimeoutPropose(v)
	return res
}

// migrateCommitStore tells the baseapp during a version upgrade, which stores to add and which
// stores to remove
func (app *App) migrateCommitStore(fromVersion, toVersion uint64) (baseapp.StoreMigrations, error) {
	oldStoreKeys := app.keyVersions[fromVersion]
	newStoreKeys := app.keyVersions[toVersion]
	result := baseapp.StoreMigrations{
		Added:   make(map[string]*storetypes.KVStoreKey),
		Deleted: make(map[string]*storetypes.KVStoreKey),
	}
	for _, oldKey := range oldStoreKeys {
		if !slices.Contains(newStoreKeys, oldKey) {
			result.Deleted[oldKey] = app.keys[oldKey]
		}
	}
	for _, newKey := range newStoreKeys {
		if !slices.Contains(oldStoreKeys, newKey) {
			result.Added[newKey] = app.keys[newKey]
		}
	}
	return result, nil
}

// migrateModules performs migrations on existing modules that have registered migrations
// between versions and initializes the state of new modules for the specified app version.
func (app *App) migrateModules(ctx sdk.Context, fromVersion, toVersion uint64) error {
	return app.manager.RunMigrations(ctx, app.configurator, fromVersion, toVersion)
}

// Info implements the ABCI interface. This method is a wrapper around baseapp's
// Info command so that it can take the app version and setup the multicommit
// store.
//
// Side-effect: calls baseapp.Init()
func (app *App) Info(req abci.RequestInfo) abci.ResponseInfo {
	if height := app.LastBlockHeight(); height > 0 {
		ctx, err := app.CreateQueryContext(height, false)
		if err != nil {
			panic(err)
		}
		appVersion := app.GetAppVersionFromParamStore(ctx)
		if appVersion > 0 {
			app.SetAppVersion(ctx, appVersion)
		} else {
			app.SetAppVersion(ctx, v1)
		}
	}

	resp := app.BaseApp.Info(req)
	// mount the stores for the provided app version
	if resp.AppVersion > 0 && !app.IsSealed() {
		app.mountKeysAndInit(resp.AppVersion)
	}
	// TODO add tiemouts to the response
	return resp
}

// InitChain implements the ABCI interface. This method is a wrapper around
// baseapp's InitChain so we can take the app version and setup the multicommit
// store.
//
// Side-effect: calls baseapp.Init()
func (app *App) InitChain(req abci.RequestInitChain) (res abci.ResponseInitChain) {
	req = setDefaultAppVersion(req)
	appVersion := req.ConsensusParams.Version.AppVersion
	// mount the stores for the provided app version if it has not already been mounted
	if app.AppVersion() == 0 && !app.IsSealed() {
		app.mountKeysAndInit(appVersion)
	}

	res = app.BaseApp.InitChain(req)

	ctx := app.NewContext(false, tmproto.Header{})
	if appVersion != v1 {
		app.SetInitialAppVersionInConsensusParams(ctx, appVersion)
		app.SetAppVersion(ctx, appVersion)
	}
	return res
}

// setDefaultAppVersion sets the default app version in the consensus params if
// it was 0. This is needed because chains (e.x. mocha-4) did not explicitly set
// an app version in genesis.json.
func setDefaultAppVersion(req abci.RequestInitChain) abci.RequestInitChain {
	if req.ConsensusParams == nil {
		panic("no consensus params set")
	}
	if req.ConsensusParams.Version == nil {
		panic("no version set in consensus params")
	}
	if req.ConsensusParams.Version.AppVersion == 0 {
		req.ConsensusParams.Version.AppVersion = v1
	}
	return req
}

// mountKeysAndInit mounts the keys for the provided app version and then
// invokes baseapp.Init().
func (app *App) mountKeysAndInit(appVersion uint64) {
	app.BaseApp.Logger().Info(fmt.Sprintf("mounting KV stores for app version %v", appVersion))
	app.MountKVStores(app.versionedKeys(appVersion))

	// Invoke load latest version for its side-effect of invoking baseapp.Init()
	if err := app.LoadLatestVersion(); err != nil {
		panic(fmt.Sprintf("loading latest version: %s", err.Error()))
	}
}

// InitChainer is middleware that gets invoked part-way through the baseapp's InitChain invocation.
func (app *App) InitChainer(ctx sdk.Context, req abci.RequestInitChain) abci.ResponseInitChain {
	var genesisState GenesisState
	if err := tmjson.Unmarshal(req.AppStateBytes, &genesisState); err != nil {
		panic(err)
	}
	appVersion := req.ConsensusParams.Version.AppVersion
	app.UpgradeKeeper.SetModuleVersionMap(ctx, app.manager.GetVersionMap(appVersion))
	return app.manager.InitGenesis(ctx, app.appCodec, genesisState, appVersion)
}

// LoadHeight loads a particular height
func (app *App) LoadHeight(height int64) error {
	return app.LoadVersion(height)
}

// SupportedVersions returns all the state machines that the
// application supports
func (app *App) SupportedVersions() []uint64 {
	return app.manager.SupportedVersions()
}

// versionedKeys returns a map from moduleName to KV store key for the given app
// version.
func (app *App) versionedKeys(appVersion uint64) map[string]*storetypes.KVStoreKey {
	output := make(map[string]*storetypes.KVStoreKey)
	if keys, exists := app.keyVersions[appVersion]; exists {
		for _, moduleName := range keys {
			if key, exists := app.keys[moduleName]; exists {
				output[moduleName] = key
			}
		}
	}
	return output
}

// baseKeys returns the base keys that are mounted to every version
func (app *App) baseKeys() map[string]*storetypes.KVStoreKey {
	return map[string]*storetypes.KVStoreKey{
		// we need to know the app version to know what stores to mount
		// thus the paramstore must always be a store that is mounted
		paramstypes.StoreKey: app.keys[paramstypes.StoreKey],
	}
}

// ModuleAccountAddrs returns all the app's module account addresses.
func (app *App) ModuleAccountAddrs() map[string]bool {
	modAccAddrs := make(map[string]bool)
	for acc := range maccPerms {
		modAccAddrs[authtypes.NewModuleAddress(acc).String()] = true
	}

	return modAccAddrs
}

// GetBaseApp implements the TestingApp interface.
func (app *App) GetBaseApp() *baseapp.BaseApp {
	return app.BaseApp
}

// GetStakingKeeper implements the TestingApp interface.
func (app *App) GetStakingKeeper() ibctestingtypes.StakingKeeper {
	return app.StakingKeeper
}

// GetIBCKeeper implements the TestingApp interface.
func (app *App) GetIBCKeeper() *ibckeeper.Keeper {
	return app.IBCKeeper
}

// GetScopedIBCKeeper implements the TestingApp interface.
func (app *App) GetScopedIBCKeeper() capabilitykeeper.ScopedKeeper {
	return app.ScopedIBCKeeper
}

// GetTxConfig implements the TestingApp interface.
func (app *App) GetTxConfig() client.TxConfig {
	return app.txConfig
}

// LegacyAmino returns SimApp's amino codec.
//
// NOTE: This is solely to be used for testing purposes as it may be desirable
// for modules to register their own custom testing types.
func (app *App) LegacyAmino() *codec.LegacyAmino {
	return app.legacyAmino
}

// AppCodec returns the app's appCodec.
//
// NOTE: This is solely to be used for testing purposes as it may be desirable
// for modules to register their own custom testing types.
func (app *App) AppCodec() codec.Codec {
	return app.appCodec
}

// InterfaceRegistry returns the app's InterfaceRegistry
func (app *App) InterfaceRegistry() types.InterfaceRegistry {
	return app.interfaceRegistry
}

// GetKey returns the KVStoreKey for the provided store key.
//
// NOTE: This is solely to be used for testing purposes.
func (app *App) GetKey(storeKey string) *storetypes.KVStoreKey {
	return app.keys[storeKey]
}

// GetTKey returns the TransientStoreKey for the provided store key.
//
// NOTE: This is solely to be used for testing purposes.
func (app *App) GetTKey(storeKey string) *storetypes.TransientStoreKey {
	return app.tkeys[storeKey]
}

// GetMemKey returns the MemStoreKey for the provided mem key.
//
// NOTE: This is solely used for testing purposes.
func (app *App) GetMemKey(storeKey string) *storetypes.MemoryStoreKey {
	return app.memKeys[storeKey]
}

// GetSubspace returns a param subspace for a given module name.
//
// NOTE: This is solely to be used for testing purposes.
func (app *App) GetSubspace(moduleName string) paramstypes.Subspace {
	subspace, _ := app.ParamsKeeper.GetSubspace(moduleName)
	return subspace
}

// RegisterAPIRoutes registers all application module routes with the provided
// API server.
func (app *App) RegisterAPIRoutes(apiSvr *api.Server, _ config.APIConfig) {
	clientCtx := apiSvr.ClientCtx
	// Register new tx routes from grpc-gateway.
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	// Register new tendermint queries routes from grpc-gateway.
	tmservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	// Register node gRPC service for grpc-gateway.
	nodeservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	ModuleBasics.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	celestiatx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
}

// RegisterTxService implements the Application.RegisterTxService method.
func (app *App) RegisterTxService(clientCtx client.Context) {
	authtx.RegisterTxService(app.BaseApp.GRPCQueryRouter(), clientCtx, app.BaseApp.Simulate, app.interfaceRegistry)
	celestiatx.RegisterTxService(app.BaseApp.GRPCQueryRouter(), clientCtx, app.interfaceRegistry)
}

// RegisterTendermintService implements the Application.RegisterTendermintService method.
func (app *App) RegisterTendermintService(clientCtx client.Context) {
	tmservice.RegisterTendermintService(clientCtx, app.BaseApp.GRPCQueryRouter(), app.interfaceRegistry, app.Query)
}

func (app *App) RegisterNodeService(clientCtx client.Context) {
	nodeservice.RegisterNodeService(clientCtx, app.GRPCQueryRouter())
}

// BlockedParams returns the params that require a hardfork to change, and
// cannot be changed via governance.
func (app *App) BlockedParams() [][2]string {
	return [][2]string{
		// bank.SendEnabled
		{banktypes.ModuleName, string(banktypes.KeySendEnabled)},
		// staking.UnbondingTime
		{stakingtypes.ModuleName, string(stakingtypes.KeyUnbondingTime)},
		// staking.BondDenom
		{stakingtypes.ModuleName, string(stakingtypes.KeyBondDenom)},
		// consensus.validator.PubKeyTypes
		{baseapp.Paramspace, string(baseapp.ParamStoreKeyValidatorParams)},
	}
}

// initParamsKeeper initializes the params keeper and its subspaces.
func initParamsKeeper(appCodec codec.BinaryCodec, legacyAmino *codec.LegacyAmino, key, tkey storetypes.StoreKey) paramskeeper.Keeper {
	paramsKeeper := paramskeeper.NewKeeper(appCodec, legacyAmino, key, tkey)

	paramsKeeper.Subspace(authtypes.ModuleName)
	paramsKeeper.Subspace(banktypes.ModuleName)
	paramsKeeper.Subspace(stakingtypes.ModuleName)
	paramsKeeper.Subspace(minttypes.ModuleName)
	paramsKeeper.Subspace(distrtypes.ModuleName)
	paramsKeeper.Subspace(slashingtypes.ModuleName)
	paramsKeeper.Subspace(govtypes.ModuleName).WithKeyTable(govv1beta2.ParamKeyTable())
	paramsKeeper.Subspace(crisistypes.ModuleName)
	paramsKeeper.Subspace(ibctransfertypes.ModuleName)
	paramsKeeper.Subspace(ibchost.ModuleName)
	paramsKeeper.Subspace(icahosttypes.SubModuleName)
	paramsKeeper.Subspace(blobtypes.ModuleName)
	paramsKeeper.Subspace(blobstreamtypes.ModuleName)
	paramsKeeper.Subspace(minfee.ModuleName)
	paramsKeeper.Subspace(packetforwardtypes.ModuleName)

	return paramsKeeper
}

func (app *App) InitializeAppVersion(ctx sdk.Context) {
	appVersion := app.GetAppVersionFromParamStore(ctx)
	if appVersion == 0 {
		// if the param store does not have an app version set, default to v1
		app.SetAppVersion(ctx, v1)
	} else {
		app.SetAppVersion(ctx, appVersion)
	}
}

// OfferSnapshot is a wrapper around the baseapp's OfferSnapshot method. It is
// needed to mount stores for the appropriate app version.
func (app *App) OfferSnapshot(req abci.RequestOfferSnapshot) abci.ResponseOfferSnapshot {
	if app.IsSealed() {
		// If the app is sealed, keys have already been mounted so this can
		// delegate to the baseapp's OfferSnapshot.
		return app.BaseApp.OfferSnapshot(req)
	}

	if app.upgradeHeightV2 == 0 {
		app.Logger().Debug("v2 upgrade height not set, assuming app version 2")
		app.mountKeysAndInit(v2)
		return app.BaseApp.OfferSnapshot(req)
	}

	if req.Snapshot.Height >= uint64(app.upgradeHeightV2) {
		app.Logger().Debug("snapshot height is greater than or equal to upgrade height, assuming app version 2")
		app.mountKeysAndInit(v2)
		return app.BaseApp.OfferSnapshot(req)
	}

	app.Logger().Debug("snapshot height is less than upgrade height, assuming app version 1")
	app.mountKeysAndInit(v1)
	return app.BaseApp.OfferSnapshot(req)
}
