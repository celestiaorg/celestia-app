package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/internal/tlsid"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cometbft/cometbft/crypto"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

const transportTestChainID = "test-chain"

// authRecorder records the auth type of each served request.
type authRecorder struct {
	types.UnimplementedFibreServer
	mu    sync.Mutex
	types []string
}

func (a *authRecorder) DownloadShard(ctx context.Context, _ *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	p, _ := peer.FromContext(ctx)
	a.mu.Lock()
	a.types = append(a.types, p.AuthInfo.AuthType())
	a.mu.Unlock()
	return &types.DownloadShardResponse{}, nil
}

func (a *authRecorder) last() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.types[len(a.types)-1]
}

// startTransportServer serves authRecorder with detecting creds, or TLS-only
// creds when tlsOnly is set.
func startTransportServer(t *testing.T, tlsOnly bool) (addr string, rec *authRecorder, pub crypto.PubKey) {
	t.Helper()
	pv := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(pv, transportTestChainID)
	require.NoError(t, err)
	pub, err = pv.GetPubKey()
	require.NoError(t, err)

	creds := credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})
	if !tlsOnly {
		creds = NewDetectingServerCreds(creds)
	}
	srv, err := Listen("127.0.0.1:0", 16, 16)
	require.NoError(t, err)
	rec = &authRecorder{}
	srv.Register(rec, grpclib.Creds(creds))
	srv.Serve()
	t.Cleanup(func() { srv.Stop(context.Background()) })
	return srv.ListenAddress(), rec, pub
}

func dialTransport(t *testing.T, addr string, creds credentials.TransportCredentials) types.FibreClient {
	t.Helper()
	conn, err := grpclib.NewClient(addr, grpclib.WithTransportCredentials(creds))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return types.NewFibreClient(conn)
}

func clientTLS(pub crypto.PubKey) credentials.TransportCredentials {
	return batchingCreds{credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // identity is checked via VerifyConnection
		VerifyConnection:   tlsid.VerifyConnection(pub, transportTestChainID),
		MinVersion:         tls.VersionTLS13,
	})}
}

func TestTransportPlaintextAndTLSOnOnePort(t *testing.T) {
	addr, rec, pub := startTransportServer(t, false)
	ctx := t.Context()

	auto := autoCreds{TransportCredentials: clientTLS(pub), modes: newTransportModes(), host: addr}
	_, err := dialTransport(t, addr, auto).DownloadShard(ctx, &types.DownloadShardRequest{})
	require.NoError(t, err)
	require.Equal(t, "insecure", rec.last())
	require.False(t, auto.modes.useTLS(addr))

	// Existing TLS-only clients keep working.
	_, err = dialTransport(t, addr, clientTLS(pub)).DownloadShard(ctx, &types.DownloadShardRequest{})
	require.NoError(t, err)
	require.Equal(t, "tls", rec.last())
}

func TestTransportFallsBackToTLS(t *testing.T) {
	addr, rec, pub := startTransportServer(t, true)

	auto := autoCreds{TransportCredentials: clientTLS(pub), modes: newTransportModes(), host: addr}
	client := dialTransport(t, addr, auto)
	// The probe fails and the same handshake falls back to TLS.
	_, err := client.DownloadShard(t.Context(), &types.DownloadShardRequest{})
	require.NoError(t, err)
	require.Equal(t, "tls", rec.last())
	require.True(t, auto.modes.useTLS(addr))

	// The TLS fallback still pins the validator identity.
	other, err := core.NewMockPV().GetPubKey()
	require.NoError(t, err)
	wrong := autoCreds{TransportCredentials: clientTLS(other), modes: auto.modes, host: addr}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = dialTransport(t, addr, wrong).DownloadShard(ctx, &types.DownloadShardRequest{})
	require.Error(t, err)
}

func TestTransportModesRetryPlaintext(t *testing.T) {
	m := newTransportModes()
	m.markTLS("h")
	require.True(t, m.useTLS("h"))
	m.tlsOnly["h"] = time.Now().Add(-time.Second)
	require.False(t, m.useTLS("h"))
}

// TestTransportRejectsBadInput checks that junk, a broken preface and silent
// clients are dropped without affecting the server.
func TestTransportRejectsBadInput(t *testing.T) {
	old := sniffTimeout
	sniffTimeout = 200 * time.Millisecond
	t.Cleanup(func() { sniffTimeout = old })
	addr, _, pub := startTransportServer(t, false)

	for name, input := range map[string][]byte{
		"junk":           []byte("GET / HTTP/1.1\r\n\r\n"),
		"broken preface": []byte("PRI * HTTP/1.1\r\n\r\nSM\r\n\r\n"),
		"short preface":  []byte("PRI"),
		"silent":         nil,
	} {
		t.Run(name, func(t *testing.T) {
			conn, err := net.Dial("tcp", addr)
			require.NoError(t, err)
			defer conn.Close()
			_, err = conn.Write(input)
			require.NoError(t, err)
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
			// The server closes (EOF) or resets the connection; a timeout means it hung.
			_, err = io.ReadAll(conn)
			var ne net.Error
			require.False(t, errors.As(err, &ne) && ne.Timeout(), "server kept the connection open")
		})
	}

	auto := autoCreds{TransportCredentials: clientTLS(pub), modes: newTransportModes(), host: addr}
	_, err := dialTransport(t, addr, auto).DownloadShard(t.Context(), &types.DownloadShardRequest{})
	require.NoError(t, err)
}
