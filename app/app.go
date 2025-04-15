package app

import (
	"fmt"
	"io"
	"os"
	"time"

	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"cosmossdk.io/x/circuit"
	circuitkeeper "cosmossdk.io/x/circuit/keeper"
	circuittypes "cosmossdk.io/x/circuit/types"
	"cosmossdk.io/x/evidence"
	evidencekeeper "cosmossdk.io/x/evidence/keeper"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	feegrantkeeper "cosmossdk.io/x/feegrant/keeper"
	feegrantmodule "cosmossdk.io/x/feegrant/module"
	"cosmossdk.io/x/upgrade"
	upgradekeeper "cosmossdk.io/x/upgrade/keeper"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	hyperlanecore "github.com/bcp-innovations/hyperlane-cosmos/x/core"
	hyperlanekeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/keeper"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/bcp-innovations/hyperlane-cosmos/x/warp"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
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
	"github.com/cosmos/cosmos-sdk/types/msgservice"
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
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/params"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
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

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/app/grpc/gasestimation"
	celestiatx "github.com/celestiaorg/celestia-app/v4/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	appv4 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v4"
	"github.com/celestiaorg/celestia-app/v4/pkg/proof"
	"github.com/celestiaorg/celestia-app/v4/x/blob"
	blobkeeper "github.com/celestiaorg/celestia-app/v4/x/blob/keeper"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
	"github.com/celestiaorg/celestia-app/v4/x/minfee"
	minfeekeeper "github.com/celestiaorg/celestia-app/v4/x/minfee/keeper"
	minfeetypes "github.com/celestiaorg/celestia-app/v4/x/minfee/types"
	"github.com/celestiaorg/celestia-app/v4/x/mint"
	mintkeeper "github.com/celestiaorg/celestia-app/v4/x/mint/keeper"
	minttypes "github.com/celestiaorg/celestia-app/v4/x/mint/types"
	"github.com/celestiaorg/celestia-app/v4/x/signal"
	signaltypes "github.com/celestiaorg/celestia-app/v4/x/signal/types"
	"github.com/celestiaorg/celestia-app/v4/x/tokenfilter"
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
	DefaultInitialVersion = appv4.Version
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
	UpgradeKeeper       *upgradekeeper.Keeper // Upgrades are set in endblock when signaled
	SignalKeeper        signal.Keeper
	MinFeeKeeper        *minfeekeeper.Keeper
	ParamsKeeper        paramskeeper.Keeper
	IBCKeeper           *ibckeeper.Keeper // IBCKeeper must be a pointer in the app, so we can SetRouter on it correctly
	EvidenceKeeper      evidencekeeper.Keeper
	TransferKeeper      ibctransferkeeper.Keeper
	FeeGrantKeeper      feegrantkeeper.Keeper
	ICAHostKeeper       icahostkeeper.Keeper
	PacketForwardKeeper *packetforwardkeeper.Keeper
	BlobKeeper          blobkeeper.Keeper
	CircuitKeeper       circuitkeeper.Keeper
	HyperlaneKeeper     hyperlanekeeper.Keeper
	WarpKeeper          warpkeeper.Keeper

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

	// the circuit keeper is used as a replacement for the message gate keeper (used in v2 and v3)
	// in order to block upgrade msg proposals.
	app.CircuitKeeper = circuitkeeper.NewKeeper(encodingConfig.Codec, runtime.NewKVStoreService(keys[circuittypes.StoreKey]), govModuleAddr, app.AccountKeeper.AddressCodec())
	app.SetCircuitBreaker(&app.CircuitKeeper)

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

	app.SignalKeeper = signal.NewKeeper(
		encodingConfig.Codec,
		keys[signaltypes.StoreKey],
		app.StakingKeeper,
	)

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

	app.GovKeeper = govkeeper.NewKeeper(
		encodingConfig.Codec, runtime.NewKVStoreService(keys[govtypes.StoreKey]), app.AccountKeeper, app.BankKeeper,
		app.StakingKeeper, app.DistrKeeper, app.MsgServiceRouter(), govtypes.DefaultConfig(), govModuleAddr,
	)
	// Set legacy router for backwards compatibility with gov v1beta1
	app.GovKeeper.SetLegacyRouter(govv1beta1.NewRouter())

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
	app.EvidenceKeeper = *evidenceKeeper

	app.BlobKeeper = *blobkeeper.NewKeeper(
		encodingConfig.Codec,
		keys[blobtypes.StoreKey],
		app.GetSubspace(blobtypes.ModuleName),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
	)

	app.MinFeeKeeper = minfeekeeper.NewKeeper(encodingConfig.Codec, keys[minfeetypes.StoreKey], app.ParamsKeeper, app.GetSubspace(minfeetypes.ModuleName), authtypes.NewModuleAddress(govtypes.ModuleName).String())

	app.PacketForwardKeeper.SetTransferKeeper(app.TransferKeeper)
	ibcRouter := ibcporttypes.NewRouter()                                                   // Create static IBC router
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transferStack)                          // Add transfer route
	ibcRouter.AddRoute(icahosttypes.SubModuleName, icahost.NewIBCModule(app.ICAHostKeeper)) // Add ICA route
	app.IBCKeeper.SetRouter(ibcRouter)

	app.HyperlaneKeeper = hyperlanekeeper.NewKeeper(
		encodingConfig.Codec,
		encodingConfig.AddressCodec,
		runtime.NewKVStoreService(keys[hyperlanetypes.ModuleName]),
		govModuleAddr,
		app.BankKeeper,
	)

	app.WarpKeeper = warpkeeper.NewKeeper(
		encodingConfig.Codec,
		encodingConfig.AddressCodec,
		runtime.NewKVStoreService(keys[warptypes.ModuleName]),
		govModuleAddr,
		app.BankKeeper,
		&app.HyperlaneKeeper,
		[]int32{int32(warptypes.HYP_TOKEN_TYPE_COLLATERAL)},
	)

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
		minfee.NewAppModule(encodingConfig.Codec, app.MinFeeKeeper),
		packetforward.NewAppModule(app.PacketForwardKeeper, app.GetSubspace(packetforwardtypes.ModuleName)),
		// ensure the light client module types are registered.
		ibctm.NewAppModule(),
		solomachine.NewAppModule(),
		circuitModule{circuit.NewAppModule(encodingConfig.Codec, app.CircuitKeeper)},
		hyperlanecore.NewAppModule(encodingConfig.Codec, &app.HyperlaneKeeper),
		warp.NewAppModule(encodingConfig.Codec, app.WarpKeeper),
	)

	// BasicModuleManager defines the module BasicManager is in charge of setting up basic,
	// non-dependant module elements, such as codec registration and genesis verification.
	// By default it is composed of all the module from the module manager.
	// Additionally, app module basics can be overwritten by passing them as argument.
	app.BasicManager = module.NewBasicManagerFromManager(
		app.ModuleManager,
		map[string]module.AppModuleBasic{
			genutiltypes.ModuleName: genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
		})
	app.BasicManager.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	app.BasicManager.RegisterLegacyAminoCodec(encodingConfig.Amino)

	// order begin block, end block and init genesis
	app.setModuleOrder()

	app.CustomQueryRouter().AddRoute(proof.TxInclusionQueryPath, proof.QueryTxInclusionProof)
	app.CustomQueryRouter().AddRoute(proof.ShareInclusionQueryPath, proof.QueryShareInclusionProof)

	app.configurator = module.NewConfigurator(encodingConfig.Codec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
		panic(err)
	}

	// RegisterUpgradeHandlers is used for registering any on-chain upgrades.
	app.RegisterUpgradeHandlers() // must be called after module manager & configuator are initialized

	// Initialize the KV stores for the base modules (e.g. params). The base modules will be included in every app version.
	app.MountKVStores(app.keys)
	app.MountMemoryStores(app.memKeys)
	app.MountTransientStores(app.tkeys)

	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
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
		app.MinFeeKeeper,
		&app.CircuitKeeper,
		app.GovParamFilters(),
	))

	protoFiles, err := proto.MergedRegistry()
	if err != nil {
		panic(err)
	}
	err = msgservice.ValidateProtoAnnotations(protoFiles)
	if err != nil {
		// Once we switch to using protoreflect-based antehandlers, we might
		// want to panic here instead of logging a warning.
		_, err := fmt.Fprintln(os.Stderr, err.Error())
		if err != nil {
			fmt.Println("could not write to stderr")
		}
	}

	app.encodingConfig = encodingConfig
	if err := app.LoadLatestVersion(); err != nil {
		panic(err)
	}

	return app
}

// Name returns the name of the App
func (app *App) Name() string { return app.BaseApp.Name() }

// Info implements the abci interface. It overrides baseapp's Info method, essentially becoming a decorator
// in order to assign TimeoutInfo values in the response.
func (app *App) Info(req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	res, err := app.BaseApp.Info(req)
	if err != nil {
		return nil, err
	}

	res.TimeoutInfo.TimeoutCommit = app.TimeoutCommit()
	res.TimeoutInfo.TimeoutPropose = app.TimeoutPropose()

	return res, nil
}

// PreBlocker application updates every pre block
func (app *App) PreBlocker(ctx sdk.Context, _ *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	return app.ModuleManager.PreBlock(ctx)
}

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
	shouldUpgrade, upgrade := app.SignalKeeper.ShouldUpgrade(ctx)
	if shouldUpgrade {
		// Version changes must be increasing. Downgrades are not permitted
		if upgrade.AppVersion > currentVersion {
			app.BaseApp.Logger().Info("upgrading app version", "current version", currentVersion, "new version", upgrade.AppVersion)

			plan := upgradetypes.Plan{
				Name:   fmt.Sprintf("v%d", upgrade.AppVersion),
				Height: upgrade.UpgradeHeight + 1, // next block is performing the upgrade.
			}

			if err := app.UpgradeKeeper.ScheduleUpgrade(ctx, plan); err != nil {
				return sdk.EndBlock{}, fmt.Errorf("failed to schedule upgrade: %v", err)
			}

			if err := app.UpgradeKeeper.DumpUpgradeInfoToDisk(upgrade.UpgradeHeight, plan); err != nil {
				return sdk.EndBlock{}, fmt.Errorf("failed to dump upgrade info to disk: %v", err)
			}

			if err := app.SetAppVersion(ctx, upgrade.AppVersion); err != nil {
				return sdk.EndBlock{}, err
			}
			app.SignalKeeper.ResetTally(ctx)

		}
	}

	res.TimeoutInfo.TimeoutCommit = app.TimeoutCommit()
	res.TimeoutInfo.TimeoutPropose = app.TimeoutPropose()

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

	res, err := app.ModuleManager.InitGenesis(ctx, app.AppCodec(), genesisState)
	if err != nil {
		return nil, err
	}

	res.TimeoutInfo.TimeoutCommit = app.TimeoutCommit()
	res.TimeoutInfo.TimeoutPropose = app.TimeoutPropose()

	return res, nil
}

// DefaultGenesis returns the default genesis state
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
	authtx.RegisterTxService(app.GRPCQueryRouter(), clientCtx, app.Simulate, app.encodingConfig.InterfaceRegistry)
	celestiatx.RegisterTxService(app.GRPCQueryRouter(), clientCtx, app.encodingConfig.InterfaceRegistry)
	gasestimation.RegisterGasEstimationService(app.GRPCQueryRouter(), clientCtx, app.encodingConfig.TxConfig.TxDecoder(), app.getGovMaxSquareBytes, app.Simulate)
}

func (app *App) getGovMaxSquareBytes() (uint64, error) {
	ctx, err := app.CreateQueryContext(app.LastBlockHeight(), false)
	if err != nil {
		return 0, err
	}
	maxSquareSize := app.BlobKeeper.GetParams(ctx).GovMaxSquareSize
	return maxSquareSize * maxSquareSize * share.ShareSize, nil
}

// RegisterTendermintService implements the Application.RegisterTendermintService method.
func (app *App) RegisterTendermintService(clientCtx client.Context) {
	tmservice.RegisterTendermintService(clientCtx, app.GRPCQueryRouter(), app.encodingConfig.InterfaceRegistry, app.Query)
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
	paramsKeeper.Subspace(govtypes.ModuleName)
	paramsKeeper.Subspace(ibctransfertypes.ModuleName)
	paramsKeeper.Subspace(ibcexported.ModuleName)
	paramsKeeper.Subspace(icahosttypes.SubModuleName)
	paramsKeeper.Subspace(blobtypes.ModuleName)
	paramsKeeper.Subspace(minfeetypes.ModuleName)
	paramsKeeper.Subspace(packetforwardtypes.ModuleName)

	return paramsKeeper
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

// TimeoutCommit returns the timeout commit duration to be used on the next block.
// It returns the user specified value as overridden by the --timeout-commit flag, otherwise
// the default timeout commit value for the current app version.
func (app *App) TimeoutCommit() time.Duration {
	if app.timeoutCommit != 0 {
		return app.timeoutCommit
	}

	return appconsts.TimeoutCommit
}

// TimeoutPropose returns the timeout propose duration to be used on the next block.
// It returns the default timeout propose value for the current app version.
func (app *App) TimeoutPropose() time.Duration {
	return appconsts.TimeoutPropose
}
