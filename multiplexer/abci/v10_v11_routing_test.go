package abci

import (
	"context"
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

// Model the embedded v10 ABCI endpoint without starting an external process.
// The real multiplexer dispatch and gRPC client perform every call in this test.
type v10UpgradeServer struct {
	abci.UnimplementedABCIServer
	finalizeCalls atomic.Int32
	commitCalls   atomic.Int32
}

func (s *v10UpgradeServer) FinalizeBlock(context.Context, *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	s.finalizeCalls.Add(1)
	return &abci.ResponseFinalizeBlock{
		AppHash:               []byte("embedded-v10"),
		ConsensusParamUpdates: &cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: 11}},
	}, nil
}

func (s *v10UpgradeServer) Commit(context.Context, *abci.RequestCommit) (*abci.ResponseCommit, error) {
	s.commitCalls.Add(1)
	return &abci.ResponseCommit{}, nil
}

func TestV10BlocksUseEmbeddedAppUntilV11UpgradeCommits(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	embedded := &v10UpgradeServer{}
	abci.RegisterABCIServer(server, embedded)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///v10",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	v10 := Version{AppVersion: 10, ABCIVersion: ABCIClientVersion2, Appd: &appd.Appd{}}
	native := &fakeApp{}
	m := &Multiplexer{
		logger: log.NewNopLogger(), versions: Versions{v10}, conn: conn,
		activeVersion: v10, started: true, appVersion: 10, nextAppVersion: 10, nativeApp: native,
	}
	resp, err := m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 100})
	require.NoError(t, err)
	require.Equal(t, []byte("embedded-v10"), resp.AppHash)
	require.EqualValues(t, 1, embedded.finalizeCalls.Load())
	require.Zero(t, native.finalizeCalls, "native v11 must not execute the v10 upgrade block")
	require.Equal(t, uint64(10), m.appVersion)
	require.Equal(t, uint64(11), m.nextAppVersion)
	selected, err := m.getApp()
	require.NoError(t, err)
	require.IsType(t, &RemoteABCIClientV2{}, selected, "v10 remains active until Commit")

	_, err = m.Commit(t.Context(), &abci.RequestCommit{})
	require.NoError(t, err)
	require.EqualValues(t, 1, embedded.commitCalls.Load(), "v10 must commit its own final block")
	require.Zero(t, native.commitCalls)
	require.Equal(t, uint64(11), m.appVersion)
	selected, err = m.getApp()
	require.NoError(t, err)
	require.Same(t, native, selected)

	_, err = m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 101})
	require.NoError(t, err)
	_, err = m.Commit(t.Context(), &abci.RequestCommit{})
	require.NoError(t, err)
	require.Equal(t, 1, native.finalizeCalls)
	require.Equal(t, 1, native.commitCalls)
	require.EqualValues(t, 1, embedded.finalizeCalls.Load())
	require.EqualValues(t, 1, embedded.commitCalls.Load())
}
