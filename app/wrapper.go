package app

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/gogo/protobuf/grpc"
	abci "github.com/lazyledger/lazyledger-core/abci/types"
)

var (
	_ abci.Application = (*wrappedBaseApp)(nil)
)

// wrappedBaseApp allows for the ability to completely "overwrite" some of the
// Baseapp's functionality, in this case, the PreProcessTxs method.
type wrappedBaseApp struct {
	bApp *baseapp.BaseApp
}

////////////////////////////////
// Info/Query Connection
////////////////////////////////

// Info implements the ABCI interface.
func (w *wrappedBaseApp) Info(req abci.RequestInfo) abci.ResponseInfo {
	return w.bApp.Info(req)
}

// Query implements the ABCI interface. It delegates to CommitMultiStore if it
// implements Queryable.
func (w *wrappedBaseApp) Query(req abci.RequestQuery) (res abci.ResponseQuery) {
	return w.bApp.Query(req)
}

////////////////////////////////
// MempoolConnection
////////////////////////////////

// CheckTx implements the ABCI interface and executes a tx in CheckTx mode. In
// CheckTx mode, messages are not executed. This means messages are only validated
// and only the AnteHandler is executed. State is persisted to the BaseApp's
// internal CheckTx state if the AnteHandler passes. Otherwise, the ResponseCheckTx
// will contain releveant error information. Regardless of tx execution outcome,
// the ResponseCheckTx will contain relevant gas execution context.
func (w *wrappedBaseApp) CheckTx(req abci.RequestCheckTx) abci.ResponseCheckTx {
	return w.bApp.CheckTx(req)
}

////////////////////////////////
// Consensus Connection
////////////////////////////////

// InitChain implements the ABCI interface. It runs the initialization logic
// directly on the CommitMultiStore.
func (w *wrappedBaseApp) InitChain(req abci.RequestInitChain) (res abci.ResponseInitChain) {
	return w.bApp.InitChain(req)
}

// BeginBlock implements the ABCI application interface.
func (w *wrappedBaseApp) BeginBlock(req abci.RequestBeginBlock) (res abci.ResponseBeginBlock) {
	return w.bApp.BeginBlock(req)
}

// DeliverTx implements the ABCI interface and executes a tx in DeliverTx mode.
// State only gets persisted if all messages are valid and get executed successfully.
// Otherwise, the ResponseDeliverTx will contain releveant error information.
// Regardless of tx execution outcome, the ResponseDeliverTx will contain relevant
// gas execution context.
func (w *wrappedBaseApp) DeliverTx(req abci.RequestDeliverTx) abci.ResponseDeliverTx {
	return w.bApp.DeliverTx(req)
}

// EndBlock implements the ABCI interface.
func (w *wrappedBaseApp) EndBlock(req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	return w.bApp.EndBlock(req)
}

// Commit implements the ABCI interface. It will commit all state that exists in
// the deliver state's multi-store and includes the resulting commit ID in the
// returned abci.ResponseCommit. Commit will set the check state based on the
// latest header and reset the deliver state. Also, if a non-zero halt height is
// defined in config, Commit will execute a deferred function call to check
// against that height and gracefully halt if it matches the latest committed
// height.
func (w *wrappedBaseApp) Commit() (res abci.ResponseCommit) {
	return w.bApp.Commit()
}

// PreprocessTxs fullfills the lazyledger-core version of the ACBI interface,
// also proposed here https://github.com/tendermint/spec/issues/194. It allows
// for arbitrary processing steps before transaction data is included in the block.
func (w *wrappedBaseApp) PreprocessTxs(txs abci.RequestPreprocessTxs) abci.ResponsePreprocessTxs {
	// todo: insert custom code here
	return w.bApp.PreprocessTxs(txs)
}

////////////////////////////////
// State Sync Connection
////////////////////////////////

// ListSnapshots implements the ABCI interface. It delegates to app.snapshotManager if set.
func (w *wrappedBaseApp) ListSnapshots(req abci.RequestListSnapshots) abci.ResponseListSnapshots {
	return w.bApp.ListSnapshots(req)
}

// LoadSnapshotChunk implements the ABCI interface. It delegates to app.snapshotManager if set.
func (w *wrappedBaseApp) LoadSnapshotChunk(req abci.RequestLoadSnapshotChunk) abci.ResponseLoadSnapshotChunk {
	return w.bApp.LoadSnapshotChunk(req)
}

// OfferSnapshot implements the ABCI interface. It delegates to app.snapshotManager if set.
func (w *wrappedBaseApp) OfferSnapshot(req abci.RequestOfferSnapshot) abci.ResponseOfferSnapshot {
	return w.bApp.OfferSnapshot(req)
}

// ApplySnapshotChunk implements the ABCI interface. It delegates to app.snapshotManager if set.
func (w *wrappedBaseApp) ApplySnapshotChunk(req abci.RequestApplySnapshotChunk) abci.ResponseApplySnapshotChunk {
	return w.bApp.ApplySnapshotChunk(req)
}

////////////////////////////////
// Server
////////////////////////////////

func (w *wrappedBaseApp) RegisterGRPCServer(server grpc.Server) {
	w.bApp.RegisterGRPCServer(server)
}
