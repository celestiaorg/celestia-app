package utils

import (
	"fmt"

	abci "github.com/tendermint/tendermint/abci/types"
)

// TODO: modify v1 state machine to contain an upgrade height and have an EndBlocker that returns with ConsensusParamsUpdates app version 2
// TODO: extend the abci.Application interface to include a method called "RunMigration"

// Multiplexer implements the abci.Application interface
var _ abci.Application = (*Multiplexer)(nil)

// Multiplexer is used to switch between different versions of the application.
type Multiplexer struct {
	currentAppVersion uint64
	// TODO: refactor apps to a map from appVersion to app
	apps           []abci.Application
	nextAppVersion uint64
}

func NewMultiplexer(currentAppVersion uint64, apps []abci.Application) *Multiplexer {
	return &Multiplexer{
		currentAppVersion: 1,
		apps:              apps,
	}
}

func (m *Multiplexer) getCurrentApp() abci.Application {
	return m.apps[m.currentAppVersion]
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

	if m.nextAppVersion != 0 {
		m.currentAppVersion = m.nextAppVersion
		m.nextAppVersion = 0
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
	// Note: the application can't create or delete stores in this method
	// because it is operating on a branch of state.
	app := m.getCurrentApp()
	got := app.EndBlock(request)
	if got.ConsensusParamUpdates.Version.AppVersion != m.currentAppVersion {
		if int(got.ConsensusParamUpdates.Version.AppVersion) >= len(m.apps) {
			panic("multiplexer: app version out of range")
		}
		fmt.Printf("setting nextAppVersion %v\n", got.ConsensusParamUpdates.Version.AppVersion)
		m.nextAppVersion = got.ConsensusParamUpdates.Version.AppVersion
	}
	return got
}

func (m *Multiplexer) Info(request abci.RequestInfo) abci.ResponseInfo {
	app := m.getCurrentApp()
	return app.Info(request)
}

func (m *Multiplexer) InitChain(request abci.RequestInitChain) abci.ResponseInitChain {
	// TODO consider getting app version from request.ConsensusParams.Version.AppVersion
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
