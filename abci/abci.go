package abci

import (
	"context"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
)

type ABCIClientVersion int

const (
	ABCIClientVersion1 ABCIClientVersion = iota
	ABCIClientVersion2
)

func (v ABCIClientVersion) String() string {
	return []string{
		"ABCIClientVersion1",
		"ABCIClientVersion2",
	}[v]
}

var _ abci.Application = (*Multiplexer)(nil)

func (m *Multiplexer) ApplySnapshotChunk(_ context.Context, req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}
	return app.ApplySnapshotChunk(req)
}

func (m *Multiplexer) CheckTx(_ context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}
	return app.CheckTx(req)
}

func (m *Multiplexer) Commit(context.Context, *abci.RequestCommit) (*abci.ResponseCommit, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}

	resp, err := app.Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}

	// after a successful commit, we start using the app version specified in FinalizeBlock.
	m.appVersion = m.nextAppVersion

	return resp, nil
}

func (m *Multiplexer) ExtendVote(ctx context.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}
	return app.ExtendVote(ctx, req)
}

func (m *Multiplexer) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}

	resp, err := app.FinalizeBlock(req)
	if err != nil {
		return nil, fmt.Errorf("failed to finalize block: %w", err)
	}

	// set the app version to be used in the next block.
	m.nextAppVersion = resp.ConsensusParamUpdates.GetVersion().App

	return resp, err
}

func (m *Multiplexer) Info(_ context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}

	return app.Info(req)
}

func (m *Multiplexer) InitChain(_ context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for genesis: %w", err)
	}
	return app.InitChain(req)
}

func (m *Multiplexer) ListSnapshots(_ context.Context, req *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}
	return app.ListSnapshots(req)
}

func (m *Multiplexer) LoadSnapshotChunk(_ context.Context, req *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}
	return app.LoadSnapshotChunk(req)
}

func (m *Multiplexer) OfferSnapshot(_ context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}
	return app.OfferSnapshot(req)
}

func (m *Multiplexer) PrepareProposal(_ context.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}
	return app.PrepareProposal(req)
}

func (m *Multiplexer) ProcessProposal(_ context.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}
	return app.ProcessProposal(req)
}

func (m *Multiplexer) Query(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}
	return app.Query(ctx, req)
}

func (m *Multiplexer) VerifyVoteExtension(_ context.Context, req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}
	return app.VerifyVoteExtension(req)
}
