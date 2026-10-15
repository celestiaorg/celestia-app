package abci

import (
	"context"
	"io"
	"net"
	"testing"

	"cosmossdk.io/log"
	db "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/stretchr/testify/require"
	abciv1 "github.com/tendermint/tendermint/abci/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// infoServer is an embedded ABCI v1 app whose Info reports appVersion.
type infoServer struct {
	abciv1.UnimplementedABCIApplicationServer
	appVersion uint64
}

func (s *infoServer) Info(context.Context, *abciv1.RequestInfo) (*abciv1.ResponseInfo, error) {
	return &abciv1.ResponseInfo{AppVersion: s.appVersion}, nil
}

// newInfoConn returns a connection to an in-memory embedded app whose Info
// reports appVersion.
func newInfoConn(t *testing.T, appVersion uint64) *grpc.ClientConn {
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	abciv1.RegisterABCIApplicationServer(srv, &infoServer{appVersion: appVersion})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestSyncAppVersionSwitchesFromEmbeddedToNative verifies that the multiplexer
// starts the native app when the embedded app has committed a newer app version.
func TestSyncAppVersionSwitchesFromEmbeddedToNative(t *testing.T) {
	native := &fakeApp{infoAppVersion: 3}
	svrCtx := server.NewDefaultContext()
	svrCtx.Config.RootDir = t.TempDir()

	m := newActivationTestMultiplexer(nil, 1)
	m.nativeApp = nil
	m.svrCtx = svrCtx
	m.appCreator = func(log.Logger, db.DB, io.Writer, servertypes.AppOptions) servertypes.Application {
		return native
	}
	m.started = true
	m.activeVersion = m.versions[0]
	m.conn = newInfoConn(t, 3)

	require.NoError(t, m.syncAppVersion())
	require.Equal(t, uint64(3), m.appVersion)
	require.Equal(t, uint64(3), m.nextAppVersion)
	require.Same(t, native, m.nativeApp)
}

func TestSyncAppVersionWithNativeApp(t *testing.T) {
	testCases := []struct {
		name           string
		appVersion     uint64
		infoAppVersion uint64
		want           uint64
	}{
		{name: "newer committed version", appVersion: 3, infoAppVersion: 4, want: 4},
		{name: "same committed version", appVersion: 3, infoAppVersion: 3, want: 3},
		{name: "older committed version", appVersion: 4, infoAppVersion: 3, want: 4},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			native := &fakeApp{infoAppVersion: tc.infoAppVersion}
			m := newActivationTestMultiplexer(native, tc.appVersion)

			require.NoError(t, m.syncAppVersion())
			require.Equal(t, tc.want, m.appVersion)
			require.Equal(t, tc.want, m.nextAppVersion)
			require.Same(t, native, m.nativeApp)
		})
	}
}
