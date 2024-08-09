package utils

import (
	"fmt"

	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	abci "github.com/tendermint/tendermint/abci/types"
)

const (
	initialAppVersion = v1.Version
)

// TODO: modify v1 state machine to contain an upgrade height and have an EndBlocker that returns with ConsensusParamsUpdates app version 2
// TODO: extend the abci.Application interface to include a method called "RunMigration"

// Multiplexer implements the abci.Application interface
var _ abci.Application = (*Multiplexer)(nil)

// Multiplexer is used to switch between different versions of the application.
type Multiplexer struct {
	// applications is a map from appVersion to application
	applications map[uint64]abci.Application
	// currentAppVersion is the version of the application that is currently running
	currentAppVersion uint64
	// nextAppVersion is the version of the application that should be upgraded to. Usually this value is the same as currentAppVersion except if the current height is an upgrade height.
	nextAppVersion uint64
}

func NewMultiplexer() *Multiplexer {
	return &Multiplexer{
		applications:      GetApplications(),
		currentAppVersion: initialAppVersion,
		nextAppVersion:    initialAppVersion,
	}
}

func (m *Multiplexer) getCurrentApp() abci.Application {
	return m.applications[m.currentAppVersion]
}

func (m *Multiplexer) ApplySnapshotChunk(request abci.RequestApplySnapshotChunk) abci.ResponseApplySnapshotChunk {
	app := m.getCurrentApp()
	return app.ApplySnapshotChunk(request)
}

func (m *Multiplexer) BeginBlock(request abci.RequestBeginBlock) abci.ResponseBeginBlock {
	app := m.getCurrentApp()
	return app.BeginBlock(request)
}

func (m *Multiplexer) CheckTx(request abci.RequestCheckTx) abci.ResponseCheckTx {
	app := m.getCurrentApp()
	return app.CheckTx(request)
}

func (m *Multiplexer) Commit() abci.ResponseCommit {
	// Note: the application can create or delete stores in this method
	app := m.getCurrentApp()
	got := app.Commit()

	if m.isUpgradePending() {
		fmt.Printf("upgrade is pending from %v to %v\n", m.currentAppVersion, m.nextAppVersion)
		m.currentAppVersion = m.nextAppVersion
		// TODO need to add RunMigrations to the abci.Application interface then uncomment:
		// app := m.getCurrentApp()
		// appHash := app.RunMigrations()
		// got.Data = appHash
		return got
	}
	return got
}

func (m *Multiplexer) DeliverTx(request abci.RequestDeliverTx) abci.ResponseDeliverTx {
	app := m.getCurrentApp()
	return app.DeliverTx(request)
}

func (m *Multiplexer) EndBlock(request abci.RequestEndBlock) abci.ResponseEndBlock {
	fmt.Printf("EndBlock height %v invoked with current app version %v\n", request.Height, m.currentAppVersion)
	// Note: the application can't create or delete stores in this method
	// because it is operating on a branch of state.
	app := m.getCurrentApp()
	got := app.EndBlock(request)
	if got.ConsensusParamUpdates != nil && got.ConsensusParamUpdates.Version != nil {
		fmt.Printf("EndBlock height %v with current app version %v next app version %v returned app version %v\n", request.Height, m.currentAppVersion, m.nextAppVersion, got.ConsensusParamUpdates.Version.AppVersion)
		if m.nextAppVersion != got.ConsensusParamUpdates.Version.AppVersion {
			if _, ok := m.applications[got.ConsensusParamUpdates.Version.AppVersion]; !ok {
				panic(fmt.Sprintf("multiplexer does not support app version %v\n", got.ConsensusParamUpdates.Version.AppVersion))
			}
			m.nextAppVersion = got.ConsensusParamUpdates.Version.AppVersion
		}
	} else {
		fmt.Printf("EndBlock height %v with current app version %v next app version %v returned nil app version\n", request.Height, m.currentAppVersion, m.nextAppVersion)
	}
	return got
}

func (m *Multiplexer) Info(request abci.RequestInfo) abci.ResponseInfo {
	app := m.getCurrentApp()
	return app.Info(request)
}

func (m *Multiplexer) InitChain(request abci.RequestInitChain) abci.ResponseInitChain {
	// TODO consider getting app version from request.ConsensusParams.Version.AppVersion
	fmt.Printf("InitChain invoked with current app version %v\n", m.currentAppVersion)
	app := m.getCurrentApp()
	return app.InitChain(request)
}

func (m *Multiplexer) ListSnapshots(request abci.RequestListSnapshots) abci.ResponseListSnapshots {
	app := m.getCurrentApp()
	return app.ListSnapshots(request)
}

func (m *Multiplexer) LoadSnapshotChunk(request abci.RequestLoadSnapshotChunk) abci.ResponseLoadSnapshotChunk {
	app := m.getCurrentApp()
	return app.LoadSnapshotChunk(request)
}

func (m *Multiplexer) OfferSnapshot(request abci.RequestOfferSnapshot) abci.ResponseOfferSnapshot {
	app := m.getCurrentApp()
	return app.OfferSnapshot(request)
}

func (m *Multiplexer) PrepareProposal(request abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	app := m.getCurrentApp()
	return app.PrepareProposal(request)
}

func (m *Multiplexer) ProcessProposal(request abci.RequestProcessProposal) abci.ResponseProcessProposal {
	app := m.getCurrentApp()
	return app.ProcessProposal(request)
}

func (m *Multiplexer) Query(request abci.RequestQuery) abci.ResponseQuery {
	app := m.getCurrentApp()
	return app.Query(request)
}

func (m *Multiplexer) SetOption(request abci.RequestSetOption) abci.ResponseSetOption {
	app := m.getCurrentApp()
	return app.SetOption(request)
}

func (m *Multiplexer) isUpgradePending() bool {
	return m.currentAppVersion != m.nextAppVersion
}
