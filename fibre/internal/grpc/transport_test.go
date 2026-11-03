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

	"github.com/celestiaorg/celestia-app/v11/fibre/internal/tlsid"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
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

// startTransportServer serves authRecorder with detecting creds, or force-TLS
// (TLS-only) creds when forceTLS is set.
func startTransportServer(t *testing.T, forceTLS bool, opts ...ServerCredsOption) (addr string, rec *authRecorder, pub crypto.PubKey) {
	t.Helper()
	pv := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(pv, transportTestChainID)
	require.NoError(t, err)
	pub, err = pv.GetPubKey()
	require.NoError(t, err)

	tlsCreds := credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})
	creds := NewDetectingServerCreds(tlsCreds, append(opts, WithServerForceTLS(forceTLS))...)
	srv, err := Listen("127.0.0.1:0", 16, 16, testAdmission)
	require.NoError(t, err)
	rec = &authRecorder{}
	srv.Register(rec, grpclib.Creds(creds))
	srv.Serve()
	t.Cleanup(func() { srv.Stop(context.Background()) })
	return srv.ListenAddress(), rec, pub
}

// dialAuto dials addr through the production plaintext-first transport (custom
// dialer plus credentials) so tests exercise the real fallback wiring.
func dialAuto(t *testing.T, addr string, tlsCreds credentials.TransportCredentials, modes *transportModes, forceTLS bool, observe DialObserver) types.FibreClient {
	t.Helper()
	transport := &autoTransport{modes: modes, host: addr, forceTLS: forceTLS, observe: observe}
	conn, err := grpclib.NewClient(addr, transport.dialOptions(tlsCreds)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return types.NewFibreClient(conn)
}

// dialTLSOnly dials addr with plain TLS credentials and no custom dialer, like
// an existing client that predates the plaintext transport.
func dialTLSOnly(t *testing.T, addr string, creds credentials.TransportCredentials) types.FibreClient {
	t.Helper()
	conn, err := grpclib.NewClient(addr, grpclib.WithTransportCredentials(creds))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return types.NewFibreClient(conn)
}

func clientTLS(pub crypto.PubKey) credentials.TransportCredentials {
	return credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // identity is checked via VerifyConnection
		VerifyConnection:   tlsid.VerifyConnection(pub, transportTestChainID),
		MinVersion:         tls.VersionTLS13,
	})
}

func TestTransportPlaintextAndTLSOnOnePort(t *testing.T) {
	addr, rec, pub := startTransportServer(t, false)
	ctx := t.Context()

	modes := newTransportModes()
	_, err := dialAuto(t, addr, clientTLS(pub), modes, false, nil).DownloadShard(ctx, &types.DownloadShardRequest{})
	require.NoError(t, err)
	require.Equal(t, "insecure", rec.last())
	require.False(t, modes.useTLS(addr))

	// Existing TLS-only clients keep working.
	_, err = dialTLSOnly(t, addr, clientTLS(pub)).DownloadShard(ctx, &types.DownloadShardRequest{})
	require.NoError(t, err)
	require.Equal(t, "tls", rec.last())
}

func TestTransportFallsBackToTLS(t *testing.T) {
	addr, rec, pub := startTransportServer(t, true)

	modes := newTransportModes()
	// The probe fails and the dialer falls back to TLS on a fresh connection.
	_, err := dialAuto(t, addr, clientTLS(pub), modes, false, nil).DownloadShard(t.Context(), &types.DownloadShardRequest{})
	require.NoError(t, err)
	require.Equal(t, "tls", rec.last())
	require.True(t, modes.useTLS(addr))

	// The TLS fallback still pins the validator identity.
	other, err := core.NewMockPV().GetPubKey()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = dialAuto(t, addr, clientTLS(other), modes, false, nil).DownloadShard(ctx, &types.DownloadShardRequest{})
	require.Error(t, err)
}

// TestTransportFallbackCancelTearsDownConn is the regression test for the
// fallback owning the connection gRPC cancels. The server fails the plaintext
// probe, completes the TLS handshake, then withholds HTTP/2 SETTINGS. Cancelling
// the dial must tear down the fallback connection in use (the server observes it
// close), not a replaced one, and leave no connection hanging.
func TestTransportFallbackCancelTearsDownConn(t *testing.T) {
	pv := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(pv, transportTestChainID)
	require.NoError(t, err)
	pub, err := pv.GetPubKey()
	require.NoError(t, err)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	closed := make(chan struct{}, 1) // the stalled TLS conn observed closing
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				var first [1]byte
				if _, err := io.ReadFull(c, first[:]); err != nil {
					_ = c.Close()
					return
				}
				if first[0] != tlsHandshakeRecord {
					// Plaintext probe: fail it so the client falls back to TLS.
					_ = c.Close()
					return
				}
				// Fallback TLS: complete the handshake, then withhold SETTINGS
				// and block until the client tears the connection down.
				tlsConn := tls.Server(&prefixConn{Conn: c, prefix: first[:]}, serverTLS)
				if err := tlsConn.Handshake(); err != nil {
					_ = c.Close()
					return
				}
				_, _ = io.Copy(io.Discard, tlsConn) // returns when the client closes it
				_ = tlsConn.Close()
				select {
				case closed <- struct{}{}:
				default:
				}
			}(raw)
		}
	}()

	addr := ln.Addr().String()
	transport := &autoTransport{modes: newTransportModes(), host: addr}
	conn, err := grpclib.NewClient(addr, transport.dialOptions(clientTLS(pub))...)
	require.NoError(t, err)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	// The RPC cannot complete (SETTINGS never arrive); it must fail by ctx.
	_, err = types.NewFibreClient(conn).DownloadShard(ctx, &types.DownloadShardRequest{})
	require.Error(t, err)

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("fallback TLS connection was not torn down after the dial was cancelled")
	}
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

	_, err := dialAuto(t, addr, clientTLS(pub), newTransportModes(), false, nil).DownloadShard(t.Context(), &types.DownloadShardRequest{})
	require.NoError(t, err)
}

// TestTransportForceTLSServerRejectsPlaintext checks that a force-TLS server
// refuses a plaintext-only client while still reporting the (rejected) attempt
// never reaches the plaintext branch.
func TestTransportForceTLSServerRejectsPlaintext(t *testing.T) {
	addr, _, pub := startTransportServer(t, true)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := dialTLSOnly(t, addr, plaintextOnlyCreds{clientTLS(pub)}).DownloadShard(ctx, &types.DownloadShardRequest{})
	require.Error(t, err)
}

// plaintextOnlyCreds dials plaintext HTTP/2 and never falls back to TLS. It
// asserts that a force-TLS server refuses plaintext.
type plaintextOnlyCreds struct {
	credentials.TransportCredentials
}

func (plaintextOnlyCreds) ClientHandshake(ctx context.Context, _ string, raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, err := probePlaintext(ctx, raw)
	if err != nil {
		return nil, nil, err
	}
	return conn, plaintextAuth, nil
}

func (c plaintextOnlyCreds) Clone() credentials.TransportCredentials { return c }

// firstByteOnWire returns the first byte the plaintext-first transport writes to
// a server, so a test can tell a TLS ClientHello from a cleartext HTTP/2
// preface. It drives the real dialer plus credentials.
func firstByteOnWire(t *testing.T, tlsCreds credentials.TransportCredentials, forceTLS bool) byte {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	got := make(chan byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var b [1]byte
		if _, err := io.ReadFull(conn, b[:]); err == nil {
			got <- b[0]
		}
	}()

	addr := ln.Addr().String()
	transport := &autoTransport{modes: newTransportModes(), host: addr, forceTLS: forceTLS}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	go func() {
		conn, err := transport.dial(ctx, addr)
		if err != nil {
			return
		}
		_, _, _ = autoCreds{TransportCredentials: tlsCreds}.ClientHandshake(ctx, "authority", conn)
	}()

	select {
	case b := <-got:
		return b
	case <-time.After(3 * time.Second):
		t.Fatal("client wrote no bytes")
		return 0
	}
}

// TestTransportForceTLSClientNeverSendsPreface checks that a force-TLS client
// opens with a TLS ClientHello and never writes the HTTP/2 preface in clear,
// while the default plaintext-first client does send the preface.
func TestTransportForceTLSClientNeverSendsPreface(t *testing.T) {
	pub, err := core.NewMockPV().GetPubKey()
	require.NoError(t, err)

	require.Equal(t, byte(tlsHandshakeRecord), firstByteOnWire(t, clientTLS(pub), true),
		"a force-TLS client must open with a TLS ClientHello, never the HTTP/2 preface")
	require.Equal(t, http2Preface[0], firstByteOnWire(t, clientTLS(pub), false),
		"the default client probes plaintext first")
}

// dialRecorder records the (mode, fallback) pairs reported to a DialObserver.
type dialRecorder struct {
	mu   sync.Mutex
	recs []struct {
		mode     string
		fallback bool
	}
}

func (r *dialRecorder) observe(mode string, fallback bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, struct {
		mode     string
		fallback bool
	}{mode, fallback})
}

func (r *dialRecorder) has(mode string, fallback bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.recs {
		if x.mode == mode && x.fallback == fallback {
			return true
		}
	}
	return false
}

// TestTransportFallbackObserved checks that the dial observer records a
// plaintext->TLS fallback against a TLS-only server, and a plain plaintext dial
// against a detecting server.
func TestTransportFallbackObserved(t *testing.T) {
	t.Run("fallback to tls", func(t *testing.T) {
		addr, rec, pub := startTransportServer(t, true)
		var dr dialRecorder
		_, err := dialAuto(t, addr, clientTLS(pub), newTransportModes(), false, dr.observe).DownloadShard(t.Context(), &types.DownloadShardRequest{})
		require.NoError(t, err)
		require.Equal(t, "tls", rec.last())
		require.True(t, dr.has(transportModeTLS, true), "a fallback dial must be observed")
	})

	t.Run("plaintext without fallback", func(t *testing.T) {
		addr, rec, pub := startTransportServer(t, false)
		var dr dialRecorder
		_, err := dialAuto(t, addr, clientTLS(pub), newTransportModes(), false, dr.observe).DownloadShard(t.Context(), &types.DownloadShardRequest{})
		require.NoError(t, err)
		require.Equal(t, "insecure", rec.last())
		require.True(t, dr.has(transportModePlaintext, false), "a plaintext dial must be observed")
	})
}

// TestDetectingCredsAcceptObserved checks that the server accept observer
// records the transport mode of plaintext and TLS connections on one port, and
// records TLS accepts under force-TLS.
func TestDetectingCredsAcceptObserved(t *testing.T) {
	record := func() (func(string), func() []string) {
		var mu sync.Mutex
		var modes []string
		return func(mode string) { mu.Lock(); modes = append(modes, mode); mu.Unlock() },
			func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), modes...) }
	}

	t.Run("mixed one port", func(t *testing.T) {
		obs, read := record()
		addr, _, pub := startTransportServer(t, false, WithAcceptObserver(obs))
		_, err := dialAuto(t, addr, clientTLS(pub), newTransportModes(), false, nil).DownloadShard(t.Context(), &types.DownloadShardRequest{})
		require.NoError(t, err)
		_, err = dialTLSOnly(t, addr, clientTLS(pub)).DownloadShard(t.Context(), &types.DownloadShardRequest{})
		require.NoError(t, err)
		require.Contains(t, read(), transportModePlaintext)
		require.Contains(t, read(), transportModeTLS)
	})

	t.Run("force-TLS still reports accepts", func(t *testing.T) {
		obs, read := record()
		addr, _, pub := startTransportServer(t, true, WithAcceptObserver(obs))
		_, err := dialTLSOnly(t, addr, clientTLS(pub)).DownloadShard(t.Context(), &types.DownloadShardRequest{})
		require.NoError(t, err)
		require.Contains(t, read(), transportModeTLS)
		require.NotContains(t, read(), transportModePlaintext)
	})
}
