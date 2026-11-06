package abci

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/multiplexer/appd"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// upgradingServer is an embedded app whose FinalizeBlock upgrades to the next
// app version and whose Info reports the block it committed.
type upgradingServer struct {
	abci.UnimplementedABCIServer
	info      *abci.ResponseInfo
	infoCalls atomic.Int32
	infoErr   error
}

func (s *upgradingServer) FinalizeBlock(context.Context, *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return &abci.ResponseFinalizeBlock{
		AppHash:               s.info.LastBlockAppHash,
		ConsensusParamUpdates: &cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: s.info.AppVersion}},
	}, nil
}

func (s *upgradingServer) Commit(context.Context, *abci.RequestCommit) (*abci.ResponseCommit, error) {
	return &abci.ResponseCommit{}, nil
}

func (s *upgradingServer) Info(context.Context, *abci.RequestInfo) (*abci.ResponseInfo, error) {
	s.infoCalls.Add(1)
	if s.infoErr != nil {
		return nil, s.infoErr
	}
	return s.info, nil
}

// errNoCommittedState stands in for the v4 binary crashing in Info when it has
// not committed a block yet (#8366).
var errNoCommittedState = errors.New("new app has not committed a block")

// freshApp is a native app that, like the v4 binary right after the v3 -> v4
// switch, cannot answer Info until it has committed its first block.
type freshApp struct {
	fakeApp
	infoCalls int
}

func (f *freshApp) Info(*abci.RequestInfo) (*abci.ResponseInfo, error) {
	f.infoCalls++
	if f.commitCalls == 0 {
		return nil, errNoCommittedState
	}
	return &abci.ResponseInfo{AppVersion: 4, LastBlockHeight: 101}, nil
}

// newSwitchTestMultiplexer returns a Multiplexer at app version 3, served by
// an in-memory embedded app, that switches to the native app at version 4.
func newSwitchTestMultiplexer(t *testing.T, embedded *upgradingServer, native *freshApp) *Multiplexer {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	abci.RegisterABCIServer(server, embedded)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///v3",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	v3 := Version{AppVersion: 3, ABCIVersion: ABCIClientVersion2, Appd: &appd.Appd{}}
	return &Multiplexer{
		logger: log.NewNopLogger(), versions: Versions{v3}, conn: conn,
		activeVersion: v3, started: true, appVersion: 3, nextAppVersion: 3, nativeApp: native,
	}
}

// TestInfoDuringVersionSwitch verifies that Info between the commit of the
// upgrade block and the first commit of the new app is answered with the
// previous app's Info, not by the new app.
func TestInfoDuringVersionSwitch(t *testing.T) {
	previousInfo := &abci.ResponseInfo{
		Data:             "celestia-app",
		Version:          "3.13.0",
		AppVersion:       4,
		LastBlockHeight:  100,
		LastBlockAppHash: []byte("v3-app-hash"),
	}
	embedded := &upgradingServer{info: previousInfo}
	native := &freshApp{}
	m := newSwitchTestMultiplexer(t, embedded, native)

	_, err := m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 100})
	require.NoError(t, err)
	_, err = m.Commit(t.Context(), &abci.RequestCommit{})
	require.NoError(t, err)
	require.Equal(t, uint64(4), m.appVersion)

	info, err := m.Info(t.Context(), &abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, previousInfo.Data, info.Data)
	require.Equal(t, previousInfo.Version, info.Version)
	require.Equal(t, previousInfo.AppVersion, info.AppVersion)
	require.Equal(t, previousInfo.LastBlockHeight, info.LastBlockHeight)
	require.Equal(t, previousInfo.LastBlockAppHash, info.LastBlockAppHash)
	require.Zero(t, native.infoCalls, "Info must not reach the new app before it commits")

	// The new app has executed but not committed its first block, so its last
	// committed height is still the previous app's.
	_, err = m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 101})
	require.NoError(t, err)
	info, err = m.Info(t.Context(), &abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, int64(100), info.LastBlockHeight)
	require.Zero(t, native.infoCalls)

	_, err = m.Commit(t.Context(), &abci.RequestCommit{})
	require.NoError(t, err)
	info, err = m.Info(t.Context(), &abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, int64(101), info.LastBlockHeight)
	require.Equal(t, 1, native.infoCalls, "Info must reach the new app once it has committed")
}

// TestInfoDuringVersionSwitchWithoutPreviousInfo verifies that Info keeps
// forwarding to the current app if the previous app's Info failed.
func TestInfoDuringVersionSwitchWithoutPreviousInfo(t *testing.T) {
	embedded := &upgradingServer{
		info:    &abci.ResponseInfo{AppVersion: 4, LastBlockHeight: 100},
		infoErr: errors.New("boom"),
	}
	native := &freshApp{}
	m := newSwitchTestMultiplexer(t, embedded, native)

	_, err := m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 100})
	require.NoError(t, err)
	_, err = m.Commit(t.Context(), &abci.RequestCommit{})
	require.NoError(t, err, "a failed Info on the previous app must not fail the commit")
	require.EqualValues(t, 1, embedded.infoCalls.Load())

	_, err = m.Info(t.Context(), &abci.RequestInfo{})
	require.ErrorIs(t, err, errNoCommittedState)
	require.Equal(t, 1, native.infoCalls)
}
