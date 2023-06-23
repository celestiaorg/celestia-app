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
	// PrepareProposalHandlerKey is the key used to retrieve the PrepareProposal handler from the
	// app options.
	PrepareProposalHandlerKey = "prepare_proposal_handler"

	// ProcessProposalHandlerKey is the key used to retrieve the ProcessProposal handler from the
	// app options.
	ProcessProposalHandlerKey = "process_proposal_handler"
)

type App struct {
	*app.App
	preparePropsoalHandler func(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal
	processProposalHandler func(req abci.RequestProcessProposal) (resp abci.ResponseProcessProposal)
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

	// default to using the good app's handlerss
	badApp.SetPrepareProposalHandler(goodApp.PrepareProposal)
	badApp.SetProcessProposalHandler(goodApp.ProcessProposal)

	// override the handlers if they are set in the app options
	if prepareHander := appOpts.Get(PrepareProposalHandlerKey); prepareHander != nil {
		badApp.SetPrepareProposalHandler(prepareHander.(func(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal))
	}
	if processHandler := appOpts.Get(ProcessProposalHandlerKey); processHandler != nil {
		badApp.SetProcessProposalHandler(processHandler.(func(req abci.RequestProcessProposal) (resp abci.ResponseProcessProposal)))
	}

	return badApp
}

func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	return app.preparePropsoalHandler(req)
}

func (app *App) ProcessProposal(req abci.RequestProcessProposal) (resp abci.ResponseProcessProposal) {
	return app.processProposalHandler(req)
}

// SetPrepareProposalHandler sets the PrepareProposal handler.
func (app *App) SetPrepareProposalHandler(handler func(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal) {
	app.preparePropsoalHandler = handler
}

// SetProcessProposalHandler sets the ProcessProposal handler.
func (app *App) SetProcessProposalHandler(handler func(req abci.RequestProcessProposal) (resp abci.ResponseProcessProposal)) {
	app.processProposalHandler = handler
}
