package multiplex

import (
	"fmt"
	"io"
	"sync"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

var _ servertypes.AppCreator = CreateMultiplexedApp

func CreateMultiplexedApp(logger log.Logger, db dbm.DB, tracer io.Writer, opts servertypes.AppOptions) servertypes.Application {
	return nil
}

var _ servertypes.Application = &Multiplexer{}

type Multiplexer struct {
	// inputs into versioned applications
	logger log.Logger
	db     dbm.DB
	tracer io.Writer
	opts   servertypes.AppOptions

	// unprotected state
	servertypes.Application
	versions map[uint64]servertypes.AppCreator

	mtx            *sync.Mutex
	currentVersion uint64
}

func (app *Multiplexer) upgradeApp(version uint64) error {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	if app.currentVersion >= version {
		return fmt.Errorf("cannot upgrade to version less than or equal to the current version: current %d desired %d", app.currentVersion, version)
	}

	creator, has := app.versions[version]
	if !has {
		return fmt.Errorf("no app creator for version %d", version)
	}

	app.Application = creator(app.logger, app.db, app.tracer, app.opts)

	app.currentVersion = version

	return nil
}

// InitChain implements the ABCI interface. It runs the initialization logic
// directly on the CommitMultiStore and initializes the appriate version of the
// application.
func (app *Multiplexer) InitChain(req abci.RequestInitChain) (res abci.ResponseInitChain) {
	version := req.ConsensusParams.Version.AppVersion

	app.mtx.Lock()
	app.currentVersion = version
	err := app.upgradeApp(app.currentVersion)
	if err != nil {
		panic(err)
	}
	app.mtx.Unlock()

	return app.InitChain(req)
}

// EndBlock implements the ABCI interface. It wraps the underlying application
// version's EndBlock to switch the underlying application upon an upgrade.
func (app *Multiplexer) EndBlock(req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	res = app.Application.EndBlock(req)
	if res.ConsensusParamUpdates != nil && res.ConsensusParamUpdates.Version != nil {
		if app.currentVersion != res.ConsensusParamUpdates.Version.AppVersion {
			err := app.upgradeApp(res.ConsensusParamUpdates.Version.AppVersion)
			// an error here must mean that the next app version is not
			// registering for multiplexing.
			if err != nil {
				panic(err)
			}
		}
	}

	return res
}
