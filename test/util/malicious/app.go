package malicious

import (
	"io"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

const (
	// BehaviorConfigKey is the key used to set the malicious config.
	BehaviorConfigKey = "behavior_config"

	// OutOfOrderHanlderKey is the key used to set the out of order prepare
	// proposal handler.
	OutOfOrderHanlderKey = "out_of_order"
)

// BehaviorConfig defines the malicious behavior for the application. It
// dictates the height at which the malicious behavior will start along with
// what type of malicious behavior will be performed.
type BehaviorConfig struct {
	// HandlerName is the name of the malicious handler to use. All known
	// handlers are defined in the PrepareProposalHandlerMap.
	HandlerName string `json:"handler_name"`
	// StartHeight is the height at which the malicious behavior will start.
	StartHeight int64 `json:"start_height"`
}

type PrepareProposalHandler func(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal

// PrepareProposalHandlerMap is a map of all the known prepare proposal handlers.
func (app *App) PrepareProposalHandlerMap() map[string]PrepareProposalHandler {
	return map[string]PrepareProposalHandler{
		OutOfOrderHanlderKey: app.OutOfOrderPrepareProposal,
	}
}

type App struct {
	*app.App
	maliciousStartHeight      int64
	malPreparePropsoalHandler PrepareProposalHandler
}

func New(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	loadLatest bool,
	skipUpgradeHeights map[int64]bool,
	homePath string,
	invCheckPeriod uint,
	encodingConfig encoding.Config,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *App {
	goodApp := app.New(logger, db, traceStore, loadLatest, skipUpgradeHeights, homePath, invCheckPeriod, encodingConfig, appOpts, baseAppOptions...)
	badApp := &App{App: goodApp}

	// set the malicious prepare proposal handler if it is set in the app options
	if malHanderName := appOpts.Get(BehaviorConfigKey); malHanderName != nil {
		badApp.SetMaliciousBehavor(malHanderName.(BehaviorConfig))
	}

	return badApp
}

func (app *App) SetMaliciousBehavor(mcfg BehaviorConfig) {
	// check if the handler is known
	if _, ok := app.PrepareProposalHandlerMap()[mcfg.HandlerName]; !ok {
		panic("unknown malicious prepare proposal handler")
	}
	app.malPreparePropsoalHandler = app.PrepareProposalHandlerMap()[mcfg.HandlerName]
	app.maliciousStartHeight = mcfg.StartHeight
}

// PrepareProposal overwrites the default app's method to use the configured
// malicious beahvior after a given height.
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	if app.LastBlockHeight()+1 >= app.maliciousStartHeight {
		return app.malPreparePropsoalHandler(req)
	}
	return app.App.PrepareProposal(req)
}

// ProcessProposal overwrites the default app's method to auto accept any
// proposal.
func (app *App) ProcessProposal(_ abci.RequestProcessProposal) (resp abci.ResponseProcessProposal) {
	return abci.ResponseProcessProposal{
		Result: abci.ResponseProcessProposal_ACCEPT,
	}
}
