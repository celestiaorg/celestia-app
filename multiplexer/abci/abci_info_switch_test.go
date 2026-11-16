package abci

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/multiplexer/appd"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
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
	// blockInfo makes Info wait until the caller gives up, like a dead
	// embedded app whose connection is still open.
	blockInfo bool
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

func (s *upgradingServer) Info(ctx context.Context, _ *abci.RequestInfo) (*abci.ResponseInfo, error) {
	s.infoCalls.Add(1)
	if s.blockInfo {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.infoErr != nil {
		return nil, s.infoErr
	}
	return s.info, nil
}

// errNoCommittedState stands in for the v4 binary crashing in Info when it has
// not committed a block yet (#8366).
var errNoCommittedState = errors.New("new app has not committed a block")

// freshApp is a native app that, like the v4 binary right after the v3 -> v4
// switch, cannot answer Info until it has committed its first block. Info is
// safe to call concurrently with Commit.
type freshApp struct {
	fakeApp
	committed atomic.Bool
	infoCalls atomic.Int32
}

func (f *freshApp) Commit() (*abci.ResponseCommit, error) {
	resp, err := f.fakeApp.Commit()
	if err == nil {
		f.committed.Store(true)
	}
	return resp, err
}

func (f *freshApp) Info(*abci.RequestInfo) (*abci.ResponseInfo, error) {
	f.infoCalls.Add(1)
	if !f.committed.Load() {
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
	require.Zero(t, native.infoCalls.Load(), "Info must not reach the new app before it commits")

	// The new app has executed but not committed its first block, so its last
	// committed height is still the previous app's.
	_, err = m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 101})
	require.NoError(t, err)
	info, err = m.Info(t.Context(), &abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, int64(100), info.LastBlockHeight)
	require.Zero(t, native.infoCalls.Load())

	_, err = m.Commit(t.Context(), &abci.RequestCommit{})
	require.NoError(t, err)
	info, err = m.Info(t.Context(), &abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, int64(101), info.LastBlockHeight)
	require.EqualValues(t, 1, native.infoCalls.Load(), "Info must reach the new app once it has committed")
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
	require.EqualValues(t, 1, native.infoCalls.Load())
}

// TestInfoDuringVersionSwitchWithUnresponsivePreviousApp verifies that Commit
// does not hang when the previous app never answers Info.
func TestInfoDuringVersionSwitchWithUnresponsivePreviousApp(t *testing.T) {
	timeout := previousAppInfoTimeout
	previousAppInfoTimeout = 50 * time.Millisecond
	t.Cleanup(func() { previousAppInfoTimeout = timeout })

	embedded := &upgradingServer{
		info:      &abci.ResponseInfo{AppVersion: 4, LastBlockHeight: 100},
		blockInfo: true,
	}
	m := newSwitchTestMultiplexer(t, embedded, &freshApp{})

	_, err := m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 100})
	require.NoError(t, err)
	_, err = m.Commit(t.Context(), &abci.RequestCommit{})
	require.NoError(t, err)
	require.EqualValues(t, 1, embedded.infoCalls.Load())
	require.Equal(t, uint64(4), m.appVersion)
}

// TestConcurrentInfoDuringVersionSwitch calls Info concurrently with the
// commit that switches app versions, as CometBFT does on its query
// connection. No Info may reach the new app before it has committed.
func TestConcurrentInfoDuringVersionSwitch(t *testing.T) {
	for range 20 {
		embedded := &upgradingServer{info: &abci.ResponseInfo{
			AppVersion: 4, LastBlockHeight: 100, LastBlockAppHash: []byte("v3-app-hash"),
		}}
		native := &freshApp{}
		m := newSwitchTestMultiplexer(t, embedded, native)

		_, err := m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 100})
		require.NoError(t, err)

		done := make(chan struct{})
		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for range 4 {
			wg.Go(func() {
				for {
					select {
					case <-done:
						return
					default:
					}
					// Info calls in flight when the switch starts are
					// cancelled, so only an error from the new app counts.
					if _, err := m.Info(t.Context(), &abci.RequestInfo{}); errors.Is(err, errNoCommittedState) {
						errs <- err
						return
					}
				}
			})
		}

		_, err = m.Commit(t.Context(), &abci.RequestCommit{})
		require.NoError(t, err)
		_, err = m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 101})
		require.NoError(t, err)
		_, err = m.Commit(t.Context(), &abci.RequestCommit{})
		require.NoError(t, err)
		close(done)
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err, "Info must not reach the new app before it commits")
		}
	}
}

// freshServer is an embedded app that, like the v4 binary right after the
// v3 -> v4 switch, cannot answer Info until it has committed its first block.
type freshServer struct {
	abci.UnimplementedABCIServer
	committed atomic.Bool
	infoCalls atomic.Int32
}

func (s *freshServer) FinalizeBlock(context.Context, *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return &abci.ResponseFinalizeBlock{AppHash: []byte("v4-app-hash")}, nil
}

func (s *freshServer) Commit(context.Context, *abci.RequestCommit) (*abci.ResponseCommit, error) {
	s.committed.Store(true)
	return &abci.ResponseCommit{}, nil
}

func (s *freshServer) Info(context.Context, *abci.RequestInfo) (*abci.ResponseInfo, error) {
	s.infoCalls.Add(1)
	if !s.committed.Load() {
		return nil, errNoCommittedState
	}
	return &abci.ResponseInfo{AppVersion: 4, LastBlockHeight: 101}, nil
}

// TestDelayedInfoDoesNotReachRestartedEmbeddedApp covers an Info call that
// selects the previous embedded app but is delayed until after the switch.
// The next embedded app serves the same address and the connection is shared,
// so if the switch did not wait for the call, it would reach the next app
// before it has committed.
func TestDelayedInfoDoesNotReachRestartedEmbeddedApp(t *testing.T) {
	newServer := func(srv abci.ABCIServer) (*grpc.Server, *bufconn.Listener) {
		listener := bufconn.Listen(1 << 20)
		server := grpc.NewServer()
		abci.RegisterABCIServer(server, srv)
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(server.Stop)
		return server, listener
	}
	v3 := &upgradingServer{info: &abci.ResponseInfo{
		AppVersion: 4, LastBlockHeight: 100, LastBlockAppHash: []byte("v3-app-hash"),
	}}
	v3Server, v3Listener := newServer(v3)
	v4 := &freshServer{}
	_, v4Listener := newServer(v4)

	// The connection dials whichever app currently serves the address.
	var serving atomic.Pointer[bufconn.Listener]
	serving.Store(v3Listener)

	// Hold the first Info call after it selected the previous app, until the
	// test releases it or its context is cancelled.
	held, release := make(chan struct{}), make(chan struct{})
	var holding atomic.Bool
	delayInfo := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if strings.HasSuffix(method, "/Info") && holding.CompareAndSwap(false, true) {
			close(held)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	conn, err := grpc.NewClient("passthrough:///app",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return serving.Load().Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(delayInfo),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	versions, err := NewVersions(
		Version{AppVersion: 3, ABCIVersion: ABCIClientVersion2, Appd: &appd.Appd{}},
		Version{AppVersion: 4, ABCIVersion: ABCIClientVersion2, Appd: newMockAppd(t, "v0.0.0-info-switch-test", mockAppdScript(filepath.Join(t.TempDir(), "v4.log")))},
	)
	require.NoError(t, err)
	m := newTestMultiplexer(t, versions, 3)
	m.conn = conn
	m.started = true
	m.activeVersion = versions[0]
	m.nextAppVersion = 3

	_, err = m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 100})
	require.NoError(t, err)

	infoErr := make(chan error, 1)
	go func() {
		_, err := m.Info(t.Context(), &abci.RequestInfo{})
		infoErr <- err
	}()
	<-held

	_, err = m.Commit(t.Context(), &abci.RequestCommit{})
	require.NoError(t, err)
	require.Equal(t, uint64(4), m.activeVersion.AppVersion)

	// The next app now serves the address.
	serving.Store(v4Listener)
	v3Server.Stop()
	// Wait until the client sees the dropped connection, so later calls
	// redial the next app instead of racing an in-flight EOF. Bound the wait:
	// if the client has already reconnected to the next app, it is Ready again
	// and there is nothing left to wait for.
	waitCtx, cancelWait := context.WithTimeout(t.Context(), 5*time.Second)
	for conn.GetState() == connectivity.Ready && conn.WaitForStateChange(waitCtx, connectivity.Ready) {
	}
	cancelWait()
	close(release)
	select {
	case <-infoErr:
	case <-time.After(10 * time.Second):
		t.Fatal("the delayed Info call did not return")
	}
	require.Zero(t, v4.infoCalls.Load(), "Info must not reach the next app before it commits")

	info, err := m.Info(t.Context(), &abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, int64(100), info.LastBlockHeight)
	require.Zero(t, v4.infoCalls.Load())

	_, err = m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 101})
	require.NoError(t, err)
	_, err = m.Commit(t.Context(), &abci.RequestCommit{})
	require.NoError(t, err)
	info, err = m.Info(t.Context(), &abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, int64(101), info.LastBlockHeight)
	require.EqualValues(t, 1, v4.infoCalls.Load())
}
