package app

import (
	"io"
	"time"

	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"cosmossdk.io/x/evidence"
	evidencekeeper "cosmossdk.io/x/evidence/keeper"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	feegrantkeeper "cosmossdk.io/x/feegrant/keeper"
	feegrantmodule "cosmossdk.io/x/feegrant/module"
	"cosmossdk.io/x/upgrade"
	upgradekeeper "cosmossdk.io/x/upgrade/keeper"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/app/grpc/gasestimation"
	celestiatx "github.com/celestiaorg/celestia-app/v4/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	appv1 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v1"
	appv2 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v2"
	appv3 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v3"
	appv4 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v4"
	"github.com/celestiaorg/celestia-app/v4/pkg/proof"
	"github.com/celestiaorg/celestia-app/v4/x/blob"
	blobkeeper "github.com/celestiaorg/celestia-app/v4/x/blob/keeper"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
	"github.com/celestiaorg/celestia-app/v4/x/minfee"
	"github.com/celestiaorg/celestia-app/v4/x/mint"
	mintkeeper "github.com/celestiaorg/celestia-app/v4/x/mint/keeper"
	minttypes "github.com/celestiaorg/celestia-app/v4/x/mint/types"
	"github.com/celestiaorg/celestia-app/v4/x/signal"
	signaltypes "github.com/celestiaorg/celestia-app/v4/x/signal/types"
	"github.com/celestiaorg/celestia-app/v4/x/tokenfilter"
	abci "github.com/cometbft/cometbft/abci/types"
	tmjson "github.com/cometbft/cometbft/libs/json"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	tmservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	runtimeservices "github.com/cosmos/cosmos-sdk/runtime/services"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
	"github.com/cosmos/cosmos-sdk/x/bank"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	consensuskeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1beta2 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/params"
	paramsclient "github.com/cosmos/cosmos-sdk/x/params/client"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	paramproposal "github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward"
	packetforwardkeeper "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/keeper"
	packetforwardtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	capabilitykeeper "github.com/cosmos/ibc-go/modules/capability/keeper"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	icahost "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host"
	icahostkeeper "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/keeper"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	"github.com/cosmos/ibc-go/v8/modules/apps/transfer"
	ibctransferkeeper "github.com/cosmos/ibc-go/v8/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibc "github.com/cosmos/ibc-go/v8/modules/core"
	ibcporttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v8/modules/core/keeper"
	solomachine "github.com/cosmos/ibc-go/v8/modules/light-clients/06-solomachine"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	ibctestingtypes "github.com/cosmos/ibc-go/v8/testing/types"
	"github.com/spf13/cast"
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
	v4                    = appv4.Version
	DefaultInitialVersion = v4
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

	encodingConfig encoding.Config

	// keys to access the substores
	keys    map[string]*storetypes.KVStoreKey
	tkeys   map[string]*storetypes.TransientStoreKey
	memKeys map[string]*storetypes.MemoryStoreKey

	// keepers
	AccountKeeper       authkeeper.AccountKeeper
	BankKeeper          bankkeeper.Keeper
	AuthzKeeper         authzkeeper.Keeper
	ConsensusKeeper     consensuskeeper.Keeper
	CapabilityKeeper    *capabilitykeeper.Keeper
	StakingKeeper       *stakingkeeper.Keeper
	SlashingKeeper      slashingkeeper.Keeper
	MintKeeper          mintkeeper.Keeper
	DistrKeeper         distrkeeper.Keeper
	GovKeeper           *govkeeper.Keeper
	UpgradeKeeper       *upgradekeeper.Keeper // This is included purely for the IBC Keeper. It is not used for upgrading
	SignalKeeper        signal.Keeper
	ParamsKeeper        paramskeeper.Keeper
	IBCKeeper           *ibckeeper.Keeper // IBCKeeper must be a pointer in the app, so we can SetRouter on it correctly
	EvidenceKeeper      evidencekeeper.Keeper
	TransferKeeper      ibctransferkeeper.Keeper
	FeeGrantKeeper      feegrantkeeper.Keeper
	ICAHostKeeper       icahostkeeper.Keeper
	PacketForwardKeeper *packetforwardkeeper.Keeper
	BlobKeeper          blobkeeper.Keeper

	ScopedIBCKeeper      capabilitykeeper.ScopedKeeper // This keeper is public for test purposes
	ScopedTransferKeeper capabilitykeeper.ScopedKeeper // This keeper is public for test purposes
	ScopedICAHostKeeper  capabilitykeeper.ScopedKeeper // This keeper is public for test purposes

	BasicManager  module.BasicManager
	ModuleManager *module.Manager
	configurator  module.Configurator
	// timeoutCommit is used to override the default timeoutCommit. This is
	// useful for testing purposes and should not be used on public networks
	// (Arabica, Mocha, or Mainnet Beta).
	timeoutCommit time.Duration
}

// New returns a reference to an uninitialized app. Callers must subsequently
// call app.Info or app.InitChain to initialize the baseapp.
func New(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	timeoutCommit time.Duration,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *App {
	encodingConfig := encoding.MakeConfig()

	baseApp := baseapp.NewBaseApp(Name, logger, db, encodingConfig.TxConfig.TxDecoder(), baseAppOptions...)
	baseApp.SetCommitMultiStoreTracer(traceStore)
	baseApp.SetVersion(version.Version)
	baseApp.SetInterfaceRegistry(encodingConfig.InterfaceRegistry)

	keys := storetypes.NewKVStoreKeys(allStoreKeys()...)
	tkeys := storetypes.NewTransientStoreKeys(paramstypes.TStoreKey)
	memKeys := storetypes.NewMemoryStoreKeys(capabilitytypes.MemStoreKey)

	govModuleAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	app := &App{
		BaseApp:       baseApp,
		keys:          keys,
		tkeys:         tkeys,
		memKeys:       memKeys,
		timeoutCommit: timeoutCommit,
	}

	// needed for migration from x/params -> module's ownership of own params
	app.ParamsKeeper = initParamsKeeper(encodingConfig.Codec, encodingConfig.Amino, keys[paramstypes.StoreKey], tkeys[paramstypes.TStoreKey])
	// only consensus keeper is global scope
	app.ConsensusKeeper = consensuskeeper.NewKeeper(encodingConfig.Codec, runtime.NewKVStoreService(keys[consensustypes.StoreKey]), govModuleAddr, runtime.EventService{})
	baseApp.SetParamStore(app.ConsensusKeeper.ParamsStore)
	baseApp.SetVersionModifier(consensus.ProvideAppVersionModifier(app.ConsensusKeeper))

	// add capability keeper and ScopeToModule for ibc module
	app.CapabilityKeeper = capabilitykeeper.NewKeeper(encodingConfig.Codec, keys[capabilitytypes.StoreKey], memKeys[capabilitytypes.MemStoreKey])

	app.ScopedIBCKeeper = app.CapabilityKeeper.ScopeToModule(ibcexported.ModuleName)
	app.ScopedTransferKeeper = app.CapabilityKeeper.ScopeToModule(ibctransfertypes.ModuleName)
	app.ScopedICAHostKeeper = app.CapabilityKeeper.ScopeToModule(icahosttypes.SubModuleName)

	app.AccountKeeper = authkeeper.NewAccountKeeper(encodingConfig.Codec, runtime.NewKVStoreService(keys[authtypes.StoreKey]), authtypes.ProtoBaseAccount, maccPerms, encodingConfig.AddressCodec, encodingConfig.AddressPrefix, govModuleAddr)

	app.BankKeeper = bankkeeper.NewBaseKeeper(
		encodingConfig.Codec,
		runtime.NewKVStoreService(keys[banktypes.StoreKey]),
		app.AccountKeeper,
		app.BlockedAddresses(),
		govModuleAddr,
		logger,
	)

	app.AuthzKeeper = authzkeeper.NewKeeper(runtime.NewKVStoreService(keys[authzkeeper.StoreKey]), encodingConfig.Codec, app.MsgServiceRouter(), app.AccountKeeper)

	app.StakingKeeper = stakingkeeper.NewKeeper(
		encodingConfig.Codec, runtime.NewKVStoreService(keys[stakingtypes.StoreKey]), app.AccountKeeper, app.BankKeeper, govModuleAddr, encodingConfig.ValidatorAddressCodec, encodingConfig.ConsensusAddressCodec,
	)

	app.MintKeeper = mintkeeper.NewKeeper(encodingConfig.Codec, keys[minttypes.StoreKey], app.StakingKeeper, app.AccountKeeper, app.BankKeeper, authtypes.FeeCollectorName)

	app.DistrKeeper = distrkeeper.NewKeeper(encodingConfig.Codec, runtime.NewKVStoreService(keys[distrtypes.StoreKey]), app.AccountKeeper, app.BankKeeper, app.StakingKeeper, authtypes.FeeCollectorName, govModuleAddr)

	app.SlashingKeeper = slashingkeeper.NewKeeper(
		encodingConfig.Codec, encodingConfig.Amino, runtime.NewKVStoreService(keys[slashingtypes.StoreKey]), app.StakingKeeper, govModuleAddr,
	)

	app.FeeGrantKeeper = feegrantkeeper.NewKeeper(encodingConfig.Codec, runtime.NewKVStoreService(keys[feegrant.StoreKey]), app.AccountKeeper)

	// The upgrade keeper is initialised solely for the ibc keeper which depends on it to know what the next validator hash is for after the
	// upgrade. This keeper is not used for the actual upgrades but merely for compatibility reasons. Ideally IBC has their own upgrade module
	// for performing IBC based upgrades. Note, as we use rolling upgrades, IBC technically never needs this functionality.
	// get skipUpgradeHeights from the app options
	skipUpgradeHeights := map[int64]bool{}
	for _, h := range cast.ToIntSlice(appOpts.Get(server.FlagUnsafeSkipUpgrades)) {
		skipUpgradeHeights[int64(h)] = true
	}
	homePath := cast.ToString(appOpts.Get(flags.FlagHome))
	app.UpgradeKeeper = upgradekeeper.NewKeeper(skipUpgradeHeights, runtime.NewKVStoreService(keys[upgradetypes.StoreKey]), encodingConfig.Codec, homePath, app.BaseApp, govModuleAddr)

	// Register the staking hooks. NOTE: stakingKeeper is passed by reference
	// above so that it will contain these hooks.
	app.StakingKeeper.SetHooks(
		stakingtypes.NewMultiStakingHooks(
			app.DistrKeeper.Hooks(),
			app.SlashingKeeper.Hooks(),
		),
	)

	app.SignalKeeper = signal.NewKeeper(encodingConfig.Codec, keys[signaltypes.StoreKey], app.StakingKeeper)

	app.IBCKeeper = ibckeeper.NewKeeper(
		encodingConfig.Codec,
		keys[ibcexported.StoreKey],
		app.GetSubspace(ibcexported.ModuleName),
		app.StakingKeeper,
		app.UpgradeKeeper,
		app.ScopedIBCKeeper,
		govModuleAddr,
	)

	app.ICAHostKeeper = icahostkeeper.NewKeeper(
		encodingConfig.Codec,
		keys[icahosttypes.StoreKey],
		app.GetSubspace(icahosttypes.SubModuleName),
		app.IBCKeeper.ChannelKeeper, // ICS4Wrapper
		app.IBCKeeper.ChannelKeeper,
		app.IBCKeeper.PortKeeper,
		app.AccountKeeper,
		app.ScopedICAHostKeeper,
		app.MsgServiceRouter(),
		govModuleAddr,
	)
	app.ICAHostKeeper.WithQueryRouter(app.GRPCQueryRouter())

	govRouter := govv1beta1.NewRouter()
	govRouter.AddRoute(govtypes.RouterKey, govv1beta1.ProposalHandler).
		AddRoute(paramproposal.RouterKey, params.NewParamChangeProposalHandler(app.ParamsKeeper))
	govConfig := govtypes.DefaultConfig()
	/*
		Example of setting gov params:
		govConfig.MaxMetadataLen = 10000
	*/
	app.GovKeeper = govkeeper.NewKeeper(
		encodingConfig.Codec, runtime.NewKVStoreService(keys[govtypes.StoreKey]), app.AccountKeeper, app.BankKeeper,
		app.StakingKeeper, app.DistrKeeper, app.MsgServiceRouter(), govConfig, govModuleAddr,
	)
	// Set legacy router for backwards compatibility with gov v1beta1
	app.GovKeeper.SetLegacyRouter(govRouter)

	// Create packet forward keeper
	app.PacketForwardKeeper = packetforwardkeeper.NewKeeper(
		encodingConfig.Codec,
		keys[packetforwardtypes.StoreKey],
		app.TransferKeeper, // will be zero-value here, reference is set later on with SetTransferKeeper.
		app.IBCKeeper.ChannelKeeper,
		app.BankKeeper,
		app.IBCKeeper.ChannelKeeper,
		govModuleAddr,
	)

	app.TransferKeeper = ibctransferkeeper.NewKeeper(
		encodingConfig.Codec, keys[ibctransfertypes.StoreKey], app.GetSubspace(ibctransfertypes.ModuleName),
		app.PacketForwardKeeper, app.IBCKeeper.ChannelKeeper, app.IBCKeeper.PortKeeper,
		app.AccountKeeper, app.BankKeeper, app.ScopedTransferKeeper, govModuleAddr,
	)
	// Transfer stack contains (from top to bottom):
	// - Token Filter
	// - Packet Forwarding Middleware
	// - Transfer
	var transferStack ibcporttypes.IBCModule
	transferStack = transfer.NewIBCModule(app.TransferKeeper)
	transferStack = packetforward.NewIBCMiddleware(transferStack, app.PacketForwardKeeper,
		0, // retries on timeout
		packetforwardkeeper.DefaultForwardTransferPacketTimeoutTimestamp, // forward timeout
	)

	// Token filter wraps packet forward middleware and is thus the first module in the transfer stack.
	transferStack = tokenfilter.NewIBCMiddleware(transferStack)

	// create evidence keeper with router
	evidenceKeeper := evidencekeeper.NewKeeper(
		encodingConfig.Codec, runtime.NewKVStoreService(keys[evidencetypes.StoreKey]), app.StakingKeeper, app.SlashingKeeper, app.AccountKeeper.AddressCodec(), runtime.ProvideCometInfoService(),
	)
	// If evidence needs to be handled for the app, set routes in router here and seal
	app.EvidenceKeeper = *evidenceKeeper

	app.BlobKeeper = *blobkeeper.NewKeeper(
		encodingConfig.Codec,
		app.GetSubspace(blobtypes.ModuleName),
	)

	app.PacketForwardKeeper.SetTransferKeeper(app.TransferKeeper)
	ibcRouter := ibcporttypes.NewRouter()                                                   // Create static IBC router
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transferStack)                          // Add transfer route
	ibcRouter.AddRoute(icahosttypes.SubModuleName, icahost.NewIBCModule(app.ICAHostKeeper)) // Add ICA route
	app.IBCKeeper.SetRouter(ibcRouter)

	/****  Module Options ****/

	// NOTE: Modules can't be modified or else must be passed by reference to the module manager
	app.ModuleManager = module.NewManager(
		genutil.NewAppModule(app.AccountKeeper, app.StakingKeeper, app, encodingConfig.TxConfig),
		auth.NewAppModule(encodingConfig.Codec, app.AccountKeeper, authsims.RandomGenesisAccounts, app.GetSubspace(authtypes.ModuleName)),
		vesting.NewAppModule(app.AccountKeeper, app.BankKeeper),
		bankModule{bank.NewAppModule(encodingConfig.Codec, app.BankKeeper, app.AccountKeeper, app.GetSubspace(banktypes.ModuleName))},
		feegrantmodule.NewAppModule(encodingConfig.Codec, app.AccountKeeper, app.BankKeeper, app.FeeGrantKeeper, encodingConfig.InterfaceRegistry),
		govModule{gov.NewAppModule(encodingConfig.Codec, app.GovKeeper, app.AccountKeeper, app.BankKeeper, app.GetSubspace(govtypes.ModuleName))},
		mintModule{mint.NewAppModule(encodingConfig.Codec, app.MintKeeper, app.AccountKeeper)},
		slashingModule{slashing.NewAppModule(encodingConfig.Codec, app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.GetSubspace(slashingtypes.ModuleName), encodingConfig.InterfaceRegistry)},
		distr.NewAppModule(encodingConfig.Codec, app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.GetSubspace(distrtypes.ModuleName)),
		stakingModule{staking.NewAppModule(encodingConfig.Codec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper, app.GetSubspace(stakingtypes.ModuleName))},
		upgrade.NewAppModule(app.UpgradeKeeper, app.AccountKeeper.AddressCodec()),
		evidence.NewAppModule(app.EvidenceKeeper),
		params.NewAppModule(app.ParamsKeeper),
		authzmodule.NewAppModule(encodingConfig.Codec, app.AuthzKeeper, app.AccountKeeper, app.BankKeeper, encodingConfig.InterfaceRegistry),
		consensus.NewAppModule(encodingConfig.Codec, app.ConsensusKeeper),
		ibcModule{ibc.NewAppModule(app.IBCKeeper)},
		transfer.NewAppModule(app.TransferKeeper),
		blob.NewAppModule(encodingConfig.Codec, app.BlobKeeper),
		signal.NewAppModule(app.SignalKeeper),
		minfee.NewAppModule(encodingConfig.Codec, app.ParamsKeeper),
		packetforward.NewAppModule(app.PacketForwardKeeper, app.GetSubspace(packetforwardtypes.ModuleName)),
		// ensure the light client module types are registered.
		ibctm.NewAppModule(),
		solomachine.NewAppModule(),
	)

	// BasicModuleManager defines the module BasicManager is in charge of setting up basic,
	// non-dependant module elements, such as codec registration and genesis verification.
	// By default it is composed of all the module from the module manager.
	// Additionally, app module basics can be overwritten by passing them as argument.
	app.BasicManager = module.NewBasicManagerFromManager(
		app.ModuleManager,
		map[string]module.AppModuleBasic{
			genutiltypes.ModuleName: genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
			govtypes.ModuleName: gov.NewAppModuleBasic(
				[]govclient.ProposalHandler{
					paramsclient.ProposalHandler,
				},
			),
		})
	app.BasicManager.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	app.BasicManager.RegisterLegacyAminoCodec(encodingConfig.Amino)

	// order begin block, end block and init genesis
	app.setModuleOrder()

	app.CustomQueryRouter().AddRoute(proof.TxInclusionQueryPath, proof.QueryTxInclusionProof)
	app.CustomQueryRouter().AddRoute(proof.ShareInclusionQueryPath, proof.QueryShareInclusionProof)

	app.configurator = module.NewConfigurator(encodingConfig.Codec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	app.ModuleManager.RegisterServices(app.configurator)

	// extract the accepted message list from the configurator and create a gatekeeper
	// which will be used both as the antehandler and as part of the circuit breaker in
	// the msg service router

	// Initialize the KV stores for the base modules (e.g. params). The base modules will be included in every app version.
	app.MountKVStores(app.keys) // TODO: this was using previously baseKeys, but we want to start from a v4 app
	app.MountMemoryStores(app.memKeys)
	app.MountTransientStores(tkeys)

	app.SetInitChainer(app.InitChainer)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
	app.SetPrepareProposal(app.PrepareProposalHandler)
	app.SetProcessProposal(app.ProcessProposalHandler)

	app.SetAnteHandler(ante.NewAnteHandler(
		app.AccountKeeper,
		app.BankKeeper,
		app.BlobKeeper,
		app.FeeGrantKeeper,
		encodingConfig.TxConfig.SignModeHandler(),
		ante.DefaultSigVerificationGasConsumer,
		app.IBCKeeper,
		app.ParamsKeeper,
		app.BlockedParamsGovernance(),
	))

	// TODO: migration related, delaying implemenation for now
	// app.SetMigrateStoreFn(app.migrateCommitStore)
	// app.SetMigrateModuleFn(app.migrateModules)

	app.encodingConfig = encodingConfig
	if err := app.LoadLatestVersion(); err != nil {
		panic(err)
	}

	return app
}

// Name returns the name of the App
func (app *App) Name() string { return app.BaseApp.Name() }

// BeginBlocker application updates every begin block
func (app *App) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	return app.ModuleManager.BeginBlock(ctx)
}

// EndBlocker executes application updates at the end of every block.
func (app *App) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	res, err := app.ModuleManager.EndBlock(ctx)
	if err != nil {
		return sdk.EndBlock{}, err
	}
	currentVersion, err := app.AppVersion(ctx)
	if err != nil {
		return sdk.EndBlock{}, err
	}

	// use a signaling mechanism for upgrade
	if shouldUpgrade, newVersion := app.SignalKeeper.ShouldUpgrade(ctx); shouldUpgrade {
		// Version changes must be increasing. Downgrades are not permitted
		if newVersion > currentVersion {
			app.BaseApp.Logger().Info("upgrading app version", "current version", currentVersion, "new version", newVersion)
			app.SetAppVersion(ctx, newVersion)
			app.SignalKeeper.ResetTally(ctx)
		}
	}

	// TODO: check if needed, it is no more there
	// res.Timeouts.TimeoutCommit = app.getTimeoutCommit(currentVersion)
	// res.Timeouts.TimeoutPropose = appconsts.GetTimeoutPropose(currentVersion)
	return res, nil
}

// InitChainer is middleware that gets invoked part-way through the baseapp's InitChain invocation.
func (app *App) InitChainer(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	var genesisState GenesisState
	if err := tmjson.Unmarshal(req.AppStateBytes, &genesisState); err != nil {
		return nil, err
	}
	versionMap := app.ModuleManager.GetVersionMap()
	if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, versionMap); err != nil {
		return nil, err
	}

	return app.ModuleManager.InitGenesis(ctx, app.AppCodec(), genesisState)
}

func (app *App) OfferSnapshot(req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	app.Logger().Info("offering snapshot", "height", req.Snapshot.Height, "app_version", req.AppVersion)
	if req.AppVersion != 0 {
		if !isSupportedAppVersion(req.AppVersion) {
			app.Logger().Info("rejecting snapshot because unsupported app version", "app_version", req.AppVersion)
			return &abci.ResponseOfferSnapshot{
				Result: abci.ResponseOfferSnapshot_REJECT,
			}, nil
		}
	}

	return app.BaseApp.OfferSnapshot(req)
}

func isSupportedAppVersion(appVersion uint64) bool {
	return appVersion == v1 || appVersion == v2 || appVersion == v3 || appVersion == v4
}

func (app *App) DefaultGenesis() GenesisState {
	return app.BasicManager.DefaultGenesis(app.encodingConfig.Codec)
}

// LoadHeight loads a particular height
func (app *App) LoadHeight(height int64) error {
	return app.LoadVersion(height)
}

// ModuleAccountAddrs returns all the app's module account addresses.
func (app *App) ModuleAccountAddrs() map[string]bool {
	modAccAddrs := make(map[string]bool)
	for acc := range maccPerms {
		modAccAddrs[authtypes.NewModuleAddress(acc).String()] = true
	}

	return modAccAddrs
}

// BlockedAddresses returns all the app's blocked account addresses.
func (app *App) BlockedAddresses() map[string]bool {
	modAccAddrs := make(map[string]bool)
	for acc := range app.ModuleAccountAddrs() {
		modAccAddrs[authtypes.NewModuleAddress(acc).String()] = true
	}

	// allow the following addresses to receive funds
	delete(modAccAddrs, authtypes.NewModuleAddress(govtypes.ModuleName).String())

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
	return app.encodingConfig.TxConfig
}

// AppCodec implements the TestingApp interface.
func (app *App) AppCodec() codec.Codec {
	return app.encodingConfig.Codec
}

// GetEncodingConfig returns the app encoding config.
func (app *App) GetEncodingConfig() encoding.Config {
	return app.encodingConfig
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
	// Register new cometbft routes from grpc-gateway.
	tmservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	// Register new tx routes from grpc-gateway.
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	// Register node gRPC service for grpc-gateway.
	nodeservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	// Register new celestia routes from grpc-gateway.
	celestiatx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	// Register grpc-gateway routes for all modules.
	app.BasicManager.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
}

// RegisterTxService implements the Application.RegisterTxService method.
func (app *App) RegisterTxService(clientCtx client.Context) {
	authtx.RegisterTxService(app.BaseApp.GRPCQueryRouter(), clientCtx, app.BaseApp.Simulate, app.encodingConfig.InterfaceRegistry)
	celestiatx.RegisterTxService(app.BaseApp.GRPCQueryRouter(), clientCtx, app.encodingConfig.InterfaceRegistry)
	gasestimation.RegisterGasEstimationService(app.BaseApp.GRPCQueryRouter(), clientCtx, app.BaseApp.Simulate)
}

// RegisterTendermintService implements the Application.RegisterTendermintService method.
func (app *App) RegisterTendermintService(clientCtx client.Context) {
	tmservice.RegisterTendermintService(
		clientCtx,
		app.BaseApp.GRPCQueryRouter(),
		app.encodingConfig.InterfaceRegistry,
		app.Query,
	)
}

func (app *App) RegisterNodeService(clientCtx client.Context, cfg config.Config) {
	nodeservice.RegisterNodeService(clientCtx, app.GRPCQueryRouter(), cfg)
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
	paramsKeeper.Subspace(ibctransfertypes.ModuleName)
	paramsKeeper.Subspace(ibcexported.ModuleName)
	paramsKeeper.Subspace(icahosttypes.SubModuleName)
	paramsKeeper.Subspace(blobtypes.ModuleName)
	paramsKeeper.Subspace(minfee.ModuleName)
	paramsKeeper.Subspace(packetforwardtypes.ModuleName)

	return paramsKeeper
}

// BlockedParamsGovernance returns the params that require a hardfork to change, and
// cannot be changed via governance.
func (app *App) BlockedParamsGovernance() map[string][]string {
	return map[string][]string{
		proto.MessageName(&banktypes.MsgUpdateParams{}):      {"send_enabled"},
		proto.MessageName(&stakingtypes.MsgUpdateParams{}):   {"params.bond_denom", "params.unbonding_time"},
		proto.MessageName(&consensustypes.MsgUpdateParams{}): {"validator"},
	}
}

// AutoCliOpts returns the autocli options for the app.
func (app *App) AutoCliOpts() autocli.AppOptions {
	modules := make(map[string]appmodule.AppModule, 0)
	for _, m := range app.ModuleManager.Modules {
		if moduleWithName, ok := m.(module.HasName); ok {
			moduleName := moduleWithName.Name()
			if appModule, ok := moduleWithName.(appmodule.AppModule); ok {
				modules[moduleName] = appModule
			}
		}
	}

	return autocli.AppOptions{
		Modules:               modules,
		ModuleOptions:         runtimeservices.ExtractAutoCLIOptions(app.ModuleManager.Modules),
		AddressCodec:          app.encodingConfig.AddressCodec,
		ValidatorAddressCodec: app.encodingConfig.ValidatorAddressCodec,
		ConsensusAddressCodec: app.encodingConfig.ConsensusAddressCodec,
	}
}

// NewProposalContext returns a context with a branched version of the state
// that is safe to query during ProcessProposal.
func (app *App) NewProposalContext(header tmproto.Header) sdk.Context {
	// use custom query multistore if provided
	ms := app.CommitMultiStore().CacheMultiStore()
	ctx := sdk.NewContext(ms, header, false, app.Logger()).
		WithBlockGasMeter(storetypes.NewInfiniteGasMeter()).
		WithBlockHeader(header)
	ctx = ctx.WithConsensusParams(app.GetConsensusParams(ctx))

	return ctx
}

// getTimeoutCommit returns the timeoutCommit if a user has overridden it via the
// --timeout-commit flag. Otherwise, it returns the default timeout commit based
// on the app version.
func (app *App) getTimeoutCommit(appVersion uint64) time.Duration {
	if app.timeoutCommit != 0 {
		return app.timeoutCommit
	}
	return appconsts.GetTimeoutCommit(appVersion)
}
