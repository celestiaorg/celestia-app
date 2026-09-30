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

	"google.golang.org/grpc/credentials"
)

// Shard traffic runs over plaintext HTTP/2 when both sides support it. Shard
// data is public, receipts are ed25519 signatures checked against the validator
// set, and downloaded rows are checked against the commitment, so TLS adds only
// cost. Servers accept both TLS and plaintext on one port; clients fall back to
// TLS for servers that only speak TLS.

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

// detectingCreds wraps server TLS credentials to also accept plaintext HTTP/2,
// chosen by the first byte of the connection.
type detectingCreds struct {
	credentials.TransportCredentials
}

// NewDetectingServerCreds returns server credentials accepting both tls and
// plaintext HTTP/2 connections.
func NewDetectingServerCreds(tls credentials.TransportCredentials) credentials.TransportCredentials {
	return detectingCreds{tls}
}

func (c detectingCreds) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
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
		return c.TransportCredentials.ServerHandshake(&prefixConn{Conn: raw, prefix: first[:]})
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
		return &prefixConn{Conn: raw, prefix: preface}, plaintextAuth, nil
	default:
		return nil, nil, fmt.Errorf("unknown protocol: first byte %#x", first[0])
	}
}

func (c detectingCreds) Clone() credentials.TransportCredentials {
	return detectingCreds{c.TransportCredentials.Clone()}
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

// autoCreds connects over plaintext HTTP/2 and falls back to tls for servers
// that do not answer the HTTP/2 preface.
type autoCreds struct {
	credentials.TransportCredentials // tls
	modes                            *transportModes
	host                             string
}

func (c autoCreds) ClientHandshake(ctx context.Context, authority string, raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if c.modes.useTLS(c.host) {
		return c.TransportCredentials.ClientHandshake(ctx, authority, raw)
	}
	conn, err := probePlaintext(ctx, raw)
	if err == nil {
		return conn, plaintextAuth, nil
	}
	// The server most likely speaks only TLS. Redial from the same local IP
	// and use TLS, so the fallback is invisible to callers.
	c.modes.markTLS(c.host)
	_ = raw.Close()
	d := net.Dialer{}
	if local, ok := raw.LocalAddr().(*net.TCPAddr); ok {
		d.LocalAddr = &net.TCPAddr{IP: local.IP}
	}
	tcp, err := d.DialContext(ctx, "tcp", raw.RemoteAddr().String())
	if err != nil {
		return nil, nil, fmt.Errorf("redialing for TLS after plaintext probe failed: %w", err)
	}
	return c.TransportCredentials.ClientHandshake(ctx, authority, tcp)
}

func (c autoCreds) Clone() credentials.TransportCredentials {
	return autoCreds{TransportCredentials: c.TransportCredentials.Clone(), modes: c.modes, host: c.host}
}

// probePlaintext sends the HTTP/2 preface and expects a SETTINGS frame back.
// The returned connection replays the frame header and drops gRPC's own
// preface write, so gRPC sees an ordinary new connection.
func probePlaintext(ctx context.Context, raw net.Conn) (net.Conn, error) {
	deadline := time.Now().Add(probeTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := raw.SetDeadline(deadline); err != nil {
		return nil, err
	}
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
	return &probedConn{prefixConn: prefixConn{Conn: raw, prefix: hdr[:]}, skip: len(http2Preface)}, nil
}

// probedConn drops the preface gRPC writes, since the probe already sent it.
type probedConn struct {
	prefixConn
	skip int
}

func (c *probedConn) Write(p []byte) (int, error) {
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
