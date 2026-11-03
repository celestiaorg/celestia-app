package grpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Shard traffic runs over plaintext HTTP/2 when both sides support it. Shard
// data is public and integrity is enforced above the transport — receipts are
// ed25519 signatures checked against the validator set and downloaded rows are
// checked against the commitment — so plaintext trades away only transport
// confidentiality and server authentication, not integrity. Servers accept both
// TLS and plaintext on one port; clients fall back to TLS for servers that only
// speak TLS.

// http2Preface is the HTTP/2 client connection preface (RFC 9113 §3.4).
const http2Preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

// tlsHandshakeRecord is the first byte of a TLS ClientHello record.
const tlsHandshakeRecord = 0x16

// sniffTimeout bounds how long a server waits for the first byte of a
// connection, or for the whole preface on plaintext. The rest of the handshake stays bounded by connectionTimeout. A
// var so tests can shorten it.
var sniffTimeout = 5 * time.Second

const (
	// probeTimeout bounds how long a client waits for a server's SETTINGS frame
	// on a plaintext connection.
	probeTimeout = 5 * time.Second
	// plaintextRetryInterval is how long a client keeps using TLS for a server
	// that failed the plaintext probe before probing again.
	plaintextRetryInterval = 10 * time.Minute
)

// Transport mode names reported to the metrics observers.
const (
	transportModePlaintext = "plaintext"
	transportModeTLS       = "tls"
)

// DialObserver is called once per client dial with the transport mode used (one
// of the transportMode* values) and whether the dial fell back from a failed
// plaintext probe to TLS. It must be safe for concurrent use.
type DialObserver func(mode string, fallback bool)

// AcceptObserver is called once per accepted server connection with the
// transport mode used (one of the transportMode* values). It must be safe for
// concurrent use.
type AcceptObserver func(mode string)

// plaintextInfo is the [credentials.AuthInfo] of a plaintext connection.
type plaintextInfo struct{ credentials.CommonAuthInfo }

func (plaintextInfo) AuthType() string { return "insecure" }

var plaintextAuth = plaintextInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.NoSecurity}}

// prefixConn replays already-read bytes before reading from the connection.
type prefixConn struct {
	net.Conn
	prefix []byte
}

func (c *prefixConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// detectingCreds wraps server TLS credentials. By default it also accepts
// plaintext HTTP/2, chosen by the first byte of the connection; with forceTLS
// it serves TLS only and rejects plaintext. Either way it reports accepts.
type detectingCreds struct {
	credentials.TransportCredentials
	observe  AcceptObserver
	forceTLS bool
}

// ServerCredsOption configures the credentials returned by
// [NewDetectingServerCreds].
type ServerCredsOption func(*detectingCreds)

// WithAcceptObserver registers o to observe the transport mode of each
// accepted connection. A nil observer is ignored.
func WithAcceptObserver(o AcceptObserver) ServerCredsOption {
	return func(c *detectingCreds) { c.observe = o }
}

// WithServerForceTLS makes the server accept TLS only and reject plaintext,
// while still reporting accepts (every one as TLS). The server-side write
// batching and buffering are unaffected.
func WithServerForceTLS(force bool) ServerCredsOption {
	return func(c *detectingCreds) { c.forceTLS = force }
}

// NewDetectingServerCreds returns server credentials accepting both tls and
// plaintext HTTP/2 connections (TLS only under [WithServerForceTLS]).
func NewDetectingServerCreds(tls credentials.TransportCredentials, opts ...ServerCredsOption) credentials.TransportCredentials {
	c := detectingCreds{TransportCredentials: tls}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

func (c detectingCreds) observeAccept(mode string) {
	if c.observe != nil {
		c.observe(mode)
	}
}

func (c detectingCreds) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if c.forceTLS {
		conn, info, err := c.TransportCredentials.ServerHandshake(raw)
		if err == nil {
			c.observeAccept(transportModeTLS)
		}
		return conn, info, err
	}

	if err := raw.SetReadDeadline(time.Now().Add(sniffTimeout)); err != nil {
		return nil, nil, err
	}
	var first [1]byte
	if _, err := io.ReadFull(raw, first[:]); err != nil {
		return nil, nil, fmt.Errorf("reading first byte: %w", err)
	}

	switch first[0] {
	case tlsHandshakeRecord:
		if err := raw.SetReadDeadline(time.Now().Add(connectionTimeout)); err != nil {
			return nil, nil, err
		}
		conn, info, err := c.TransportCredentials.ServerHandshake(&prefixConn{Conn: raw, prefix: first[:]})
		if err == nil {
			c.observeAccept(transportModeTLS)
		}
		return conn, info, err
	case http2Preface[0]:
		preface := make([]byte, len(http2Preface))
		preface[0] = first[0]
		if _, err := io.ReadFull(raw, preface[1:]); err != nil {
			return nil, nil, fmt.Errorf("reading HTTP/2 preface: %w", err)
		}
		if string(preface) != http2Preface {
			return nil, nil, errors.New("invalid HTTP/2 preface")
		}
		if err := raw.SetReadDeadline(time.Now().Add(connectionTimeout)); err != nil {
			return nil, nil, err
		}
		c.observeAccept(transportModePlaintext)
		return &prefixConn{Conn: raw, prefix: preface}, plaintextAuth, nil
	default:
		return nil, nil, fmt.Errorf("unknown protocol: first byte %#x", first[0])
	}
}

func (c detectingCreds) Clone() credentials.TransportCredentials {
	return detectingCreds{TransportCredentials: c.TransportCredentials.Clone(), observe: c.observe, forceTLS: c.forceTLS}
}

// transportModes remembers servers that failed the plaintext probe.
type transportModes struct {
	mu      sync.Mutex
	tlsOnly map[string]time.Time // host -> when to probe plaintext again
}

func newTransportModes() *transportModes {
	return &transportModes{tlsOnly: make(map[string]time.Time)}
}

func (m *transportModes) useTLS(host string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	until, ok := m.tlsOnly[host]
	if ok && time.Now().After(until) {
		delete(m.tlsOnly, host)
		return false
	}
	return ok
}

func (m *transportModes) markTLS(host string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tlsOnly[host] = time.Now().Add(plaintextRetryInterval)
}

// autoTransport dials shard connections plaintext-first with a transparent TLS
// fallback. The protocol choice happens in the dialer, so the connection gRPC
// owns — and tears down if HTTP/2 setup is cancelled — is the one actually used:
// the dialer returns the final connection and [autoCreds] only runs the TLS
// handshake when that connection is not already plaintext.
//
// Connections are dialed directly with a [net.Dialer]; fibre reaches validators
// by the IP:port in the on-chain host registry, so there is no HTTP CONNECT
// proxy in the path for either the plaintext probe or the TLS connection. A
// custom dialer bypasses gRPC's proxy support uniformly, so plaintext and TLS
// behave the same — there is no silent proxy difference between the first dial
// and a fallback.
type autoTransport struct {
	modes    *transportModes
	host     string
	forceTLS bool
	observe  DialObserver
}

// dialOptions returns the gRPC dial options that install the plaintext-first
// dialer and the credentials that complete a TLS handshake only for TLS
// connections. tls is the identity-pinned TLS credentials.
func (t *autoTransport) dialOptions(tls credentials.TransportCredentials) []grpclib.DialOption {
	return []grpclib.DialOption{
		grpclib.WithContextDialer(t.dial),
		grpclib.WithTransportCredentials(autoCreds{TransportCredentials: tls}),
	}
}

func (t *autoTransport) observeDial(mode string, fallback bool) {
	if t.observe != nil {
		t.observe(mode, fallback)
	}
}

func (t *autoTransport) dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	tcp, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if t.forceTLS || t.modes.useTLS(t.host) {
		t.observeDial(transportModeTLS, false)
		return tcp, nil
	}
	pc, err := probePlaintext(ctx, tcp)
	if err == nil {
		t.observeDial(transportModePlaintext, false)
		return pc, nil
	}
	if ctx.Err() != nil {
		// The dial was cancelled, which says nothing about the server's
		// protocol; fail without caching TLS-only or redialing.
		_ = tcp.Close()
		return nil, ctx.Err()
	}
	// The server most likely speaks only TLS. Redial from the same local IP and
	// hand the connection back for the TLS handshake. gRPC owns this connection,
	// so a cancel during the TLS or HTTP/2 setup tears down the connection in
	// use rather than a replaced one.
	t.modes.markTLS(t.host)
	_ = tcp.Close()
	redial := net.Dialer{}
	if local, ok := tcp.LocalAddr().(*net.TCPAddr); ok {
		redial.LocalAddr = &net.TCPAddr{IP: local.IP}
	}
	tcp2, err := redial.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("redialing for TLS after plaintext probe failed: %w", err)
	}
	t.observeDial(transportModeTLS, true)
	return tcp2, nil
}

// autoCreds runs the TLS handshake over a TLS connection and treats a
// [plaintextConn] from the dialer as already-established plaintext HTTP/2.
type autoCreds struct {
	credentials.TransportCredentials // tls
}

func (c autoCreds) ClientHandshake(ctx context.Context, authority string, raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if _, ok := raw.(*plaintextConn); ok {
		return raw, plaintextAuth, nil
	}
	return c.TransportCredentials.ClientHandshake(ctx, authority, raw)
}

func (c autoCreds) Clone() credentials.TransportCredentials {
	return autoCreds{TransportCredentials: c.TransportCredentials.Clone()}
}

// probePlaintext sends the HTTP/2 preface and expects a SETTINGS frame back.
// The returned connection replays the frame header and drops gRPC's own
// preface write, so gRPC sees an ordinary new connection.
func probePlaintext(ctx context.Context, raw net.Conn) (*plaintextConn, error) {
	deadline := time.Now().Add(probeTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := raw.SetDeadline(deadline); err != nil {
		return nil, err
	}
	// Unblock the probe read/write if the dial context is cancelled before its
	// deadline.
	stop := context.AfterFunc(ctx, func() { _ = raw.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	if _, err := raw.Write([]byte(http2Preface)); err != nil {
		return nil, err
	}
	var hdr [9]byte
	if _, err := io.ReadFull(raw, hdr[:]); err != nil {
		return nil, err
	}
	// Frame header: 24-bit length, type, flags, stream ID. The server's first
	// frame must be a non-ACK SETTINGS frame on stream 0.
	const frameSettings, flagAck = 0x4, 0x1
	if hdr[3] != frameSettings || hdr[4]&flagAck != 0 || !bytes.Equal(hdr[5:9], []byte{0, 0, 0, 0}) {
		return nil, fmt.Errorf("unexpected first frame %x", hdr)
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return &plaintextConn{prefixConn: prefixConn{Conn: raw, prefix: hdr[:]}, skip: len(http2Preface)}, nil
}

// plaintextConn is the dialer's result for a server that answered the plaintext
// probe. It replays the server's first frame header and drops the preface gRPC
// writes, since the probe already sent it. [autoCreds] recognises this type and
// skips the TLS handshake.
type plaintextConn struct {
	prefixConn
	skip int
}

func (c *plaintextConn) Write(p []byte) (int, error) {
	if c.skip == 0 {
		return c.Conn.Write(p)
	}
	n := min(c.skip, len(p))
	sent := len(http2Preface) - c.skip
	if string(p[:n]) != http2Preface[sent:sent+n] {
		return 0, errors.New("unexpected write before HTTP/2 preface")
	}
	c.skip -= n
	if n == len(p) {
		return n, nil
	}
	m, err := c.Conn.Write(p[n:])
	return n + m, err
}
