package abci

import (
	"context"

	"google.golang.org/grpc"

	abci "github.com/cometbft/cometbft/abci/types"
)

type RemoteABCIClientV2 struct {
	abci.ABCIClient
}

// NewRemoteABCIClientV2 returns a new ABCI Client (using ABCI v2).
// The client behaves like CometBFT for the server side (the application side).
func NewRemoteABCIClientV2(conn *grpc.ClientConn) *RemoteABCIClientV2 {
	return &RemoteABCIClientV2{ABCIClient: abci.NewABCIClient(conn)}
}

// ApplySnapshotChunk implements abci.ABCI.
func (a *RemoteABCIClientV2) ApplySnapshotChunk(req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	return a.ABCIClient.ApplySnapshotChunk(context.Background(), req, grpc.WaitForReady(true))
}

// CheckTx implements abci.ABCI.
func (a *RemoteABCIClientV2) CheckTx(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	return a.ABCIClient.CheckTx(context.Background(), req, grpc.WaitForReady(true))
}

// Commit implements abci.ABCI.
func (a *RemoteABCIClientV2) Commit() (*abci.ResponseCommit, error) {
	return a.ABCIClient.Commit(context.Background(), &abci.RequestCommit{}, grpc.WaitForReady(true))
}

// ExtendVote implements abci.ABCI.
func (a *RemoteABCIClientV2) ExtendVote(ctx context.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
	return a.ABCIClient.ExtendVote(ctx, req, grpc.WaitForReady(true))
}

// FinalizeBlock implements abci.ABCI.
func (a *RemoteABCIClientV2) FinalizeBlock(req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return a.ABCIClient.FinalizeBlock(context.Background(), req, grpc.WaitForReady(true))
}

// Info implements abci.ABCI.
func (a *RemoteABCIClientV2) Info(req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	return a.ABCIClient.Info(context.Background(), req, grpc.WaitForReady(true))
}

// InitChain implements abci.ABCI.
func (a *RemoteABCIClientV2) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return a.ABCIClient.InitChain(context.Background(), req, grpc.WaitForReady(true))
}

// ListSnapshots implements abci.ABCI.
func (a *RemoteABCIClientV2) ListSnapshots(req *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
	return a.ABCIClient.ListSnapshots(context.Background(), req, grpc.WaitForReady(true))
}

// LoadSnapshotChunk implements abci.ABCI.
func (a *RemoteABCIClientV2) LoadSnapshotChunk(req *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
	return a.ABCIClient.LoadSnapshotChunk(context.Background(), req, grpc.WaitForReady(true))
}

// OfferSnapshot implements abci.ABCI.
func (a *RemoteABCIClientV2) OfferSnapshot(req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	return a.ABCIClient.OfferSnapshot(context.Background(), req, grpc.WaitForReady(true))
}

// PrepareProposal implements abci.ABCI.
func (a *RemoteABCIClientV2) PrepareProposal(req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	return a.ABCIClient.PrepareProposal(context.Background(), req, grpc.WaitForReady(true))
}

// ProcessProposal implements abci.ABCI.
func (a *RemoteABCIClientV2) ProcessProposal(req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	return a.ABCIClient.ProcessProposal(context.Background(), req, grpc.WaitForReady(true))
}

// Query implements abci.ABCI.
func (a *RemoteABCIClientV2) Query(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
	return a.ABCIClient.Query(ctx, req, grpc.WaitForReady(true))
}

// VerifyVoteExtension implements abci.ABCI.
func (a *RemoteABCIClientV2) VerifyVoteExtension(req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
	return a.ABCIClient.VerifyVoteExtension(context.Background(), req, grpc.WaitForReady(true))

}
