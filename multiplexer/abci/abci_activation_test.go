package abci

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v9/multiplexer/appd"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/stretchr/testify/require"
)

// fakeApp is a native app whose FinalizeBlock and Commit return preset results.
type fakeApp struct {
	servertypes.Application
	// upgradeTo is the app version FinalizeBlock reports. Zero reports none.
	upgradeTo uint64
	// infoAppVersion is the app version Info reports.
	infoAppVersion uint64
	finalizeErr    error
	commitErr      error
	finalizeCalls  int
	commitCalls    int
}

func (f *fakeApp) FinalizeBlock(*abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	f.finalizeCalls++
	if f.finalizeErr != nil {
		return nil, f.finalizeErr
	}
	resp := &abci.ResponseFinalizeBlock{}
	if f.upgradeTo != 0 {
		resp.ConsensusParamUpdates = &cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: f.upgradeTo}}
	}
	return resp, nil
}

func (f *fakeApp) Info(*abci.RequestInfo) (*abci.ResponseInfo, error) {
	return &abci.ResponseInfo{AppVersion: f.infoAppVersion}, nil
}

func (f *fakeApp) Commit() (*abci.ResponseCommit, error) {
	f.commitCalls++
	if f.commitErr != nil {
		return nil, f.commitErr
	}
	return &abci.ResponseCommit{}, nil
}

// newActivationTestMultiplexer returns a Multiplexer at appVersion with app
// versions 1 and 2 embedded and later versions served by native.
func newActivationTestMultiplexer(native *fakeApp, appVersion uint64) *Multiplexer {
	return &Multiplexer{
		logger: log.NewNopLogger(),
		versions: Versions{
			{AppVersion: 1, ABCIVersion: ABCIClientVersion1, Appd: &appd.Appd{}},
			{AppVersion: 2, ABCIVersion: ABCIClientVersion2},
		},
		nativeApp:      native,
		appVersion:     appVersion,
		nextAppVersion: appVersion,
	}
}

func TestFinalizeBlockSelectsNextAppVersion(t *testing.T) {
	native := &fakeApp{upgradeTo: 4}
	m := newActivationTestMultiplexer(native, 3)

	_, err := m.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
	require.NoError(t, err)
	require.Equal(t, 1, native.finalizeCalls)
	require.Equal(t, uint64(4), m.nextAppVersion)
	require.Equal(t, uint64(3), m.appVersion, "the next version must not be active before Commit")
}

func TestFinalizeBlockWithoutVersionUpdateKeepsNextAppVersion(t *testing.T) {
	m := newActivationTestMultiplexer(&fakeApp{}, 3)

	_, err := m.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
	require.NoError(t, err)
	require.Equal(t, uint64(3), m.nextAppVersion)
}

func TestFailedFinalizeBlockLeavesNextAppVersionUnchanged(t *testing.T) {
	t.Run("app error", func(t *testing.T) {
		native := &fakeApp{upgradeTo: 4, finalizeErr: errors.New("boom")}
		m := newActivationTestMultiplexer(native, 3)

		_, err := m.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
		require.ErrorContains(t, err, "failed to finalize block: boom")
		require.Equal(t, uint64(3), m.nextAppVersion)
	})

	t.Run("halt height reached", func(t *testing.T) {
		native := &fakeApp{upgradeTo: 4}
		m := newActivationTestMultiplexer(native, 3)
		m.svrCfg = serverconfig.Config{BaseConfig: serverconfig.BaseConfig{HaltHeight: 5}}

		_, err := m.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 5})
		require.ErrorContains(t, err, "halting node per configuration at height 5")
		require.Zero(t, native.finalizeCalls, "the app must not be called when halting")
		require.Equal(t, uint64(3), m.nextAppVersion)
	})
}

func TestCommitActivatesNextAppVersion(t *testing.T) {
	native := &fakeApp{upgradeTo: 4}
	m := newActivationTestMultiplexer(native, 3)

	_, err := m.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
	require.NoError(t, err)
	_, err = m.Commit(context.Background(), &abci.RequestCommit{})
	require.NoError(t, err)

	require.Equal(t, 1, native.commitCalls)
	require.Equal(t, uint64(4), m.appVersion)
}

func TestFailedCommitLeavesAppVersionUnchanged(t *testing.T) {
	native := &fakeApp{upgradeTo: 4, commitErr: errors.New("boom")}
	m := newActivationTestMultiplexer(native, 3)

	_, err := m.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
	require.NoError(t, err)
	_, err = m.Commit(context.Background(), &abci.RequestCommit{})
	require.ErrorContains(t, err, "failed to commit: boom")

	require.Equal(t, uint64(3), m.appVersion)
	require.Equal(t, uint64(4), m.nextAppVersion)
}

// TestCommitRoutesFromEmbeddedToNativeApp verifies that committing the last
// block of an embedded version routes later calls to the native app.
func TestCommitRoutesFromEmbeddedToNativeApp(t *testing.T) {
	native := &fakeApp{}
	m := newActivationTestMultiplexer(native, 1)
	m.started = true
	m.activeVersion = m.versions[0]

	app, err := m.getApp()
	require.NoError(t, err)
	require.IsType(t, &RemoteABCIClientV1{}, app)

	// RemoteABCIClientV1.Commit makes no remote call, so this exercises the
	// embedded path without a running binary.
	m.nextAppVersion = 3
	_, err = m.Commit(context.Background(), &abci.RequestCommit{})
	require.NoError(t, err)
	require.Equal(t, uint64(3), m.appVersion)

	_, err = m.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 2})
	require.NoError(t, err)
	require.Equal(t, 1, native.finalizeCalls, "FinalizeBlock must be routed to the native app")
}

// TestCommitReturnsErrorWhenUpgradeFails verifies that Commit reports an
// error when the next app version cannot be started. The app has already
// committed, so the next version stays selected.
func TestCommitReturnsErrorWhenUpgradeFails(t *testing.T) {
	m := newActivationTestMultiplexer(&fakeApp{}, 1)
	m.started = true
	m.activeVersion = m.versions[0]
	m.nextAppVersion = 2 // registered without an Appd, so it cannot start

	_, err := m.Commit(context.Background(), &abci.RequestCommit{})
	require.ErrorContains(t, err, "multiplexer failed upgrade")
	require.Equal(t, uint64(2), m.appVersion)
}
