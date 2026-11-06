package abci

import (
	"bytes"
	"context"
	"fmt"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
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

	// after a successful commit, start using the app version specified in FinalizeBlock. If
	// there is an upgrade, perform that now.
	m.mu.Lock()
	oldAppVersion, nextAppVersion := m.appVersion, m.nextAppVersion
	m.mu.Unlock()

	// The next app has not committed a block yet. Its Info would read the last
	// committed height, which the previous app wrote, and the v4 binary
	// crashes doing so because the stores it adds at the upgrade do not exist
	// at that height. Answer Info for this window from the previous app, which
	// reports exactly that height. See #8366.
	var previousInfo *abci.ResponseInfo
	if oldAppVersion != nextAppVersion {
		previousInfo = m.previousAppInfo(app)
	}

	// Switch the app version and the Info answer together so that Info never
	// selects the next app before it has committed. Without a switch this
	// clears the stored answer, because the app serving appVersion has now
	// committed a block of its own.
	m.mu.Lock()
	m.switchInfo = previousInfo
	m.appVersion = nextAppVersion
	m.mu.Unlock()

	if oldAppVersion != nextAppVersion {
		// this effectively performs the upgrade immediately instead of waiting until the next call to getApp.
		_, err = m.getApp()
		if err != nil {
			return nil, fmt.Errorf("multiplexer failed upgrade: %w", err)
		}
	}

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
	err := m.checkHaltConditions(req)
	if err != nil {
		// It is not possible to shutdown the multiplexer via m.Stop() here
		// because CometBFT is blocked on the response from this method. So this
		// just returns an error to CometBFT and the user must exit the process
		// manually (via CTRL + C). This error results in a consensus failure on
		// v3.x which matches the behavior on v4.x. The node state will remain
		// intact after the consensus failure so the user can continue syncing
		// after the consensus failure here.
		return nil, fmt.Errorf("failed to finalize block because the node should halt: %w", err)
	}

	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}

	resp, err := app.FinalizeBlock(req)
	if err != nil {
		return nil, fmt.Errorf("failed to finalize block: %w", err)
	}

	// set the app version to be used in the next block.
	if resp.ConsensusParamUpdates != nil && resp.ConsensusParamUpdates.GetVersion() != nil {
		m.mu.Lock()
		m.nextAppVersion = resp.ConsensusParamUpdates.GetVersion().App
		m.mu.Unlock()
	}

	return resp, err
}

func (m *Multiplexer) Info(_ context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	// Check the stored answer and select the app in one critical section, so
	// that a concurrent Commit cannot switch app versions in between.
	m.mu.Lock()
	if m.switchInfo != nil {
		info := *m.switchInfo
		info.LastBlockAppHash = bytes.Clone(m.switchInfo.LastBlockAppHash)
		m.mu.Unlock()
		return &info, nil
	}
	app, err := m.getAppLocked()
	appVersion := m.appVersion
	m.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", appVersion, err)
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
	m.mu.Lock()
	m.appVersion = req.AppVersion
	m.nextAppVersion = req.AppVersion
	m.mu.Unlock()

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

func (m *Multiplexer) QuerySequence(ctx context.Context, req *abci.RequestQuerySequence) (*abci.ResponseQuerySequence, error) {
	// Return noop (sequence 0) for app version <= 4
	if m.appVersion <= 4 {
		return &abci.ResponseQuerySequence{Sequence: 0}, nil
	}

	app, err := m.getApp()
	if err != nil {
		return nil, fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}

	return app.QuerySequence(ctx, req)
}

// previousAppInfoTimeout bounds the Info call to the previous app in Commit,
// so a dead embedded app whose connection is still open cannot block Commit.
var previousAppInfoTimeout = 5 * time.Second

// previousAppInfo returns the Info response of app, which has just committed
// the last block of its app version. It returns nil if the call fails, in
// which case Info keeps forwarding to the current app.
func (m *Multiplexer) previousAppInfo(app servertypes.ABCI) *abci.ResponseInfo {
	ctx, cancel := context.WithTimeout(context.Background(), previousAppInfoTimeout)
	defer cancel()

	var (
		info *abci.ResponseInfo
		err  error
	)
	switch client := app.(type) {
	case *RemoteABCIClientV1, *RemoteABCIClientV2:
		// The connection is closed if the embedded app exited. Commit through
		// RemoteABCIClientV1 makes no remote call, so it can succeed anyway.
		m.mu.Lock()
		connected := m.conn != nil
		m.mu.Unlock()
		if !connected {
			m.logger.Error("no connection to the previous app to get its info before switching app versions")
			return nil
		}
		if v1, ok := client.(*RemoteABCIClientV1); ok {
			info, err = v1.infoWithContext(ctx, &abci.RequestInfo{})
		} else {
			info, err = client.(*RemoteABCIClientV2).infoWithContext(ctx, &abci.RequestInfo{})
		}
	default:
		info, err = app.Info(&abci.RequestInfo{})
	}
	if err != nil {
		m.logger.Error("failed to get info from the previous app before switching app versions", "err", err)
		return nil
	}
	return info
}

// checkHaltConditions returns an error if the node should halt based on a halt-height or
// halt-time configured in app.toml.
func (m *Multiplexer) checkHaltConditions(req *abci.RequestFinalizeBlock) error {
	if m.svrCfg.HaltHeight > 0 && uint64(req.Height) >= m.svrCfg.HaltHeight {
		return fmt.Errorf("halting node per configuration at height %v", m.svrCfg.HaltHeight)
	}
	if m.svrCfg.HaltTime > 0 && req.Time.Unix() >= int64(m.svrCfg.HaltTime) {
		return fmt.Errorf("halting node per configuration at time %v", m.svrCfg.HaltTime)
	}
	return nil
}
