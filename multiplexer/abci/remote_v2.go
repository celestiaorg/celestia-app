package abci

import (
	"context"
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/grpc"
)

type RemoteABCIClientV2 struct {
	abci.ABCIClient
}

// NewRemoteABCIClientV2 returns a new ABCI Client (using ABCI v2).
// The client behaves like CometBFT for the server side (the application side).
func NewRemoteABCIClientV2(conn *grpc.ClientConn) *RemoteABCIClientV2 {
	return &RemoteABCIClientV2{
		ABCIClient: abci.NewABCIClient(conn),
	}
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
	fmt.Printf("[DEBUG] multiplexer/abci/remote_v2.go Info invoked\n")
	fmt.Printf("[DEBUG] Request: Version=%s, BlockVersion=%d, P2PVersion=%d\n", req.Version, req.BlockVersion, req.P2PVersion)
	fmt.Printf("[DEBUG] ABCIClient type: %T\n", a.ABCIClient)
	fmt.Printf("[DEBUG] Calling a.ABCIClient.Info() with gRPC WaitForReady option\n")
	resp, err := a.ABCIClient.Info(context.Background(), req, grpc.WaitForReady(true))
	if err != nil {
		fmt.Printf("[DEBUG] multiplexer/abci/remote_v2.go Info error: %v\n", err)
	} else {
		fmt.Printf("[DEBUG] multiplexer/abci/remote_v2.go Info result: Data=%s, Version=%s, AppVersion=%d, LastBlockHeight=%d\n",
			resp.Data, resp.Version, resp.AppVersion, resp.LastBlockHeight)
	}
	return resp, err
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

// QuerySequence implements abci.ABCI.
func (a *RemoteABCIClientV2) QuerySequence(ctx context.Context, req *abci.RequestQuerySequence) (*abci.ResponseQuerySequence, error) {
	return a.ABCIClient.QuerySequence(ctx, req, grpc.WaitForReady(true))
}
