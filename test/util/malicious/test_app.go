package malicious

import (
	"io"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

// OutOfOrderNamesapceConfig returns a testnode config that will start producing
// blocks with out of order namespaces at the provided height.
//
// Note: per the OutOfOrder go docs, the first two blobs with different
// namespaces will be swapped, resulting in an invalid block.
func OutOfOrderNamespaceConfig(startHeight int64) *testnode.Config {
	bcfg := BehaviorConfig{StartHeight: startHeight, HandlerName: OutOfOrderHandlerKey}
	return TestNodeConfig(bcfg)
}

// TestNodeConfig returns a testnode config with the malicious application and
// provided behavior set in the app options.
func TestNodeConfig(behavior BehaviorConfig) *testnode.Config {
	cfg := testnode.DefaultConfig().
		WithAppCreator(NewAppServer)

	cfg.AppOptions.Set(BehaviorConfigKey, behavior)
	return cfg
}

// NewTestApp creates a new malicious application with the provided consensus
// params.
func NewTestApp(cparams *tmproto.ConsensusParams, mcfg BehaviorConfig, genAccounts ...string) *App {
	app, _ := util.SetupTestAppWithGenesisValSet(cparams, genAccounts...)
	badapp := &App{App: app}
	badapp.SetMaliciousBehavior(mcfg)
	return badapp
}

// NewAppServer creates a new AppServer using the malicious application.
func NewAppServer(logger log.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
	return New(
		logger, db, traceStore,
		appOpts,
		server.DefaultBaseappOptions(appOpts)...,
	)
}
