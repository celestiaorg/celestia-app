package grpc

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/internal/tlsid"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/credentials"
)

type countingConn struct {
	net.Conn
	writes atomic.Int64
}

func (c *countingConn) Write(p []byte) (int, error) {
	c.writes.Add(1)
	return c.Conn.Write(p)
}

// TestBatchingCredsOneSyscallPerWrite checks that a multi-record TLS write
// reaches the socket in one write, intact, and that peer verification still runs.
func TestBatchingCredsOneSyscallPerWrite(t *testing.T) {
	const chainID = "test-chain"
	pv := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(pv, chainID)
	require.NoError(t, err)
	pub, err := pv.GetPubKey()
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	// Deadlines make a batching regression fail fast instead of hanging the suite.
	const timeout = 10 * time.Second
	deadline := time.Now().Add(timeout)

	payload := make([]byte, uploadWriteBufferSize)
	_, _ = rand.Read(payload)
	received := make(chan []byte, 2)
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			_ = raw.SetDeadline(deadline)
			go func() {
				srv := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2"}})
				defer srv.Close()
				buf := make([]byte, len(payload))
				if _, err := io.ReadFull(srv, buf); err != nil {
					buf = nil
				}
				received <- buf
			}()
		}
	}()

	dial := func(verify func(tls.ConnectionState) error) (net.Conn, *countingConn, error) {
		raw, err := net.Dial("tcp", ln.Addr().String())
		require.NoError(t, err)
		require.NoError(t, raw.SetDeadline(deadline))
		counting := &countingConn{Conn: raw}
		creds := batchingCreds{credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // identity is checked via VerifyConnection
			VerifyConnection:   verify,
			MinVersion:         tls.VersionTLS13,
		})}
		conn, _, err := creds.ClientHandshake(t.Context(), "validator", counting)
		return conn, counting, err
	}

	conn, counting, err := dial(tlsid.VerifyConnection(pub, chainID))
	require.NoError(t, err)
	defer conn.Close()

	before := counting.writes.Load()
	n, err := conn.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.Equal(t, int64(1), counting.writes.Load()-before)
	select {
	case got := <-received:
		require.True(t, bytes.Equal(payload, got))
	case <-time.After(timeout):
		t.Fatal("server did not receive the payload")
	}

	// A wrong identity still fails the handshake.
	other, err := core.NewMockPV().GetPubKey()
	require.NoError(t, err)
	_, _, err = dial(tlsid.VerifyConnection(other, chainID))
	require.Error(t, err)
}
