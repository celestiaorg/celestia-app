package app

import (
	circuittypes "cosmossdk.io/x/circuit/types"
	"cosmossdk.io/x/evidence"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	feegrantmodule "cosmossdk.io/x/feegrant/module"
	"cosmossdk.io/x/upgrade"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	hyperlanecore "github.com/bcp-innovations/hyperlane-cosmos/x/core"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/bcp-innovations/hyperlane-cosmos/x/warp"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v6/x/blob"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/celestia-app/v6/x/minfee"
	minfeetypes "github.com/celestiaorg/celestia-app/v6/x/minfee/types"
	minttypes "github.com/celestiaorg/celestia-app/v6/x/mint/types"
	"github.com/celestiaorg/celestia-app/v6/x/signal"
	signaltypes "github.com/celestiaorg/celestia-app/v6/x/signal/types"
	"github.com/celestiaorg/celestia-app/v6/x/zkism"
	zkismtypes "github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	"github.com/cosmos/cosmos-sdk/x/distribution"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/params"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward"
	packetforwardtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	"github.com/cosmos/ibc-go/modules/capability"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	"github.com/cosmos/ibc-go/v8/modules/apps/transfer"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	solomachine "github.com/cosmos/ibc-go/v8/modules/light-clients/06-solomachine"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
)

// ModuleEncodingRegisters keeps track of all the module methods needed to
// register interfaces and specific type to encoding config
var ModuleEncodingRegisters = []module.AppModuleBasic{
	// cosmos-sdk std
	auth.AppModuleBasic{},
	authzmodule.AppModuleBasic{},
	bankModule{},
	capability.AppModuleBasic{},
	circuitModule{},
	consensus.AppModuleBasic{},
	distribution.AppModuleBasic{},
	evidence.AppModuleBasic{},
	feegrantmodule.AppModuleBasic{},
	genutil.AppModuleBasic{},
	govModule{},
	params.AppModuleBasic{},
	slashingModule{},
	stakingModule{},
	upgrade.AppModuleBasic{},
	vesting.AppModuleBasic{},
	// ibc
	ibcModule{},
	transfer.AppModuleBasic{},
	packetforward.AppModuleBasic{},
	icaModule{},
	ibctm.AppModuleBasic{},
	solomachine.AppModuleBasic{},
	// hyperlane
	hyperlanecore.AppModule{},
	warp.AppModule{},
	zkism.AppModule{},
	// celestia
	blob.AppModule{},
	minfee.AppModule{},
	mintModule{},
	signal.AppModule{},
}

func (app *App) setModuleOrder() {
	// During begin block slashing happens after distr.BeginBlocker so that
	// there is nothing left over in the validator fee pool, so as to keep the
	// CanWithdrawInvariant invariant.
	// NOTE: staking module is required if HistoricalEntries param > 0
	app.ModuleManager.SetOrderBeginBlockers(
		capabilitytypes.ModuleName,
		minttypes.ModuleName,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		evidencetypes.ModuleName,
		stakingtypes.ModuleName,
		ibcexported.ModuleName,
		ibctransfertypes.ModuleName,
		genutiltypes.ModuleName,
		blobtypes.ModuleName,
		paramstypes.ModuleName,
		authz.ModuleName,
		signaltypes.ModuleName,
		minfeetypes.ModuleName,
		icatypes.ModuleName,
		packetforwardtypes.ModuleName,
		zkismtypes.ModuleName,
	)

	app.ModuleManager.SetOrderPreBlockers(
		upgradetypes.ModuleName,
	)

	app.ModuleManager.SetOrderEndBlockers(
		govtypes.ModuleName,
		stakingtypes.ModuleName,
		capabilitytypes.ModuleName,
		minttypes.ModuleName,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		evidencetypes.ModuleName,
		ibcexported.ModuleName,
		ibctransfertypes.ModuleName,
		feegrant.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		genutiltypes.ModuleName,
		blobtypes.ModuleName,
		paramstypes.ModuleName,
		authz.ModuleName,
		vestingtypes.ModuleName,
		signaltypes.ModuleName,
		minfeetypes.ModuleName,
		packetforwardtypes.ModuleName,
		icatypes.ModuleName,
	)

	// NOTE: The genutils module must occur after staking so that pools are
	// properly initialized with tokens from genesis accounts.
	// NOTE: Capability module must occur first so that it can initialize any capabilities
	// so that other modules that want to create or claim capabilities afterwards in InitChain
	// can do so safely.
	// NOTE: The minfee module must occur before genutil so DeliverTx can
	// successfully pass the fee checking logic
	app.ModuleManager.SetOrderInitGenesis(
		capabilitytypes.ModuleName,
		consensustypes.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		distrtypes.ModuleName,
		stakingtypes.ModuleName,
		slashingtypes.ModuleName,
		govtypes.ModuleName,
		minttypes.ModuleName,
		ibcexported.ModuleName,
		minfeetypes.ModuleName,
		genutiltypes.ModuleName,
		evidencetypes.ModuleName,
		ibctransfertypes.ModuleName,
		blobtypes.ModuleName,
		vestingtypes.ModuleName,
		feegrant.ModuleName,
		paramstypes.ModuleName,
		authz.ModuleName,
		signaltypes.ModuleName,
		packetforwardtypes.ModuleName,
		icatypes.ModuleName,
		upgradetypes.ModuleName,
		circuittypes.ModuleName,
		hyperlanetypes.ModuleName,
		warptypes.ModuleName,
		zkismtypes.ModuleName,
	)
}

func allStoreKeys() []string {
	return []string{
		authtypes.StoreKey,
		authzkeeper.StoreKey,
		banktypes.StoreKey,
		stakingtypes.StoreKey,
		minttypes.StoreKey,
		distrtypes.StoreKey,
		slashingtypes.StoreKey,
		govtypes.StoreKey,
		paramstypes.StoreKey,
		upgradetypes.StoreKey,
		feegrant.StoreKey,
		evidencetypes.StoreKey,
		capabilitytypes.StoreKey,
		ibctransfertypes.StoreKey,
		ibcexported.StoreKey,
		packetforwardtypes.StoreKey,
		icahosttypes.StoreKey,
		signaltypes.StoreKey,
		blobtypes.StoreKey,
		minfeetypes.StoreKey,      // added in v4
		consensustypes.StoreKey,   // added in v4
		circuittypes.StoreKey,     // added in v4
		hyperlanetypes.ModuleName, // added in v4
		warptypes.ModuleName,      // added in v4
		zkismtypes.StoreKey,
	}
}
