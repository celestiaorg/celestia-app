package main

import abci "github.com/tendermint/tendermint/abci/types"

// Multiplexer implements the abci.Application interface
var _ abci.Application = (*Multiplexer)(nil)

type Multiplexer struct {
	currentAppVersion uint64
	apps              []abci.Application
}

func NewMultiplexer(currentAppVersion uint64, applications ...abci.Application) *Multiplexer {
	return &Multiplexer{
		currentAppVersion: 1,
		apps:              applications,
	}
}

// ApplySnapshotChunk implements types.Application.
func (m *Multiplexer) ApplySnapshotChunk(abci.RequestApplySnapshotChunk) abci.ResponseApplySnapshotChunk {
	panic("unimplemented")
}

// BeginBlock implements types.Application.
func (m *Multiplexer) BeginBlock(abci.RequestBeginBlock) abci.ResponseBeginBlock {
	panic("unimplemented")
}

// CheckTx implements types.Application.
func (m *Multiplexer) CheckTx(abci.RequestCheckTx) abci.ResponseCheckTx {
	panic("unimplemented")
}

// Commit implements types.Application.
func (m *Multiplexer) Commit() abci.ResponseCommit {
	panic("unimplemented")
}

// DeliverTx implements types.Application.
func (m *Multiplexer) DeliverTx(abci.RequestDeliverTx) abci.ResponseDeliverTx {
	panic("unimplemented")
}

// EndBlock implements types.Application.
func (m *Multiplexer) EndBlock(abci.RequestEndBlock) abci.ResponseEndBlock {
	panic("unimplemented")
}

// Info implements types.Application.
func (m *Multiplexer) Info(abci.RequestInfo) abci.ResponseInfo {
	panic("unimplemented")
}

// InitChain implements types.Application.
func (m *Multiplexer) InitChain(abci.RequestInitChain) abci.ResponseInitChain {
	panic("unimplemented")
}

// ListSnapshots implements types.Application.
func (m *Multiplexer) ListSnapshots(abci.RequestListSnapshots) abci.ResponseListSnapshots {
	panic("unimplemented")
}

// LoadSnapshotChunk implements types.Application.
func (m *Multiplexer) LoadSnapshotChunk(abci.RequestLoadSnapshotChunk) abci.ResponseLoadSnapshotChunk {
	panic("unimplemented")
}

// OfferSnapshot implements types.Application.
func (m *Multiplexer) OfferSnapshot(abci.RequestOfferSnapshot) abci.ResponseOfferSnapshot {
	panic("unimplemented")
}

// PrepareProposal implements types.Application.
func (m *Multiplexer) PrepareProposal(abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	panic("unimplemented")
}

// ProcessProposal implements types.Application.
func (m *Multiplexer) ProcessProposal(abci.RequestProcessProposal) abci.ResponseProcessProposal {
	panic("unimplemented")
}

// Query implements types.Application.
func (m *Multiplexer) Query(abci.RequestQuery) abci.ResponseQuery {
	panic("unimplemented")
}

// SetOption implements types.Application.
func (m *Multiplexer) SetOption(abci.RequestSetOption) abci.ResponseSetOption {
	panic("unimplemented")
}
