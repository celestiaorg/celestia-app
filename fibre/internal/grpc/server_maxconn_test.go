package grpc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// TestLimitedListenerFailFast: a connection over the global or per-IP cap is
// closed at once rather than parked, and a closed connection frees its slot.
func TestLimitedListenerFailFast(t *testing.T) {
	for name, caps := range map[string]struct {
		global int64
		perIP  int
	}{
		"global": {global: 2},
		"per-IP": {global: 100, perIP: 2},
	} {
		t.Run(name, func(t *testing.T) {
			inner, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			ln := newLimitedListener(inner, caps.global, caps.perIP)
			t.Cleanup(func() { _ = ln.Close() })

			accepted := make(chan net.Conn, 8)
			go func() {
				for {
					c, err := ln.Accept()
					if err != nil {
						return
					}
					accepted <- c
				}
			}()
			addr := inner.Addr().String()

			dialTCP(t, addr)
			first := mustAccept(t, accepted)
			dialTCP(t, addr)
			mustAccept(t, accepted)

			over := dialTCP(t, addr)
			require.NoError(t, over.SetReadDeadline(time.Now().Add(2*time.Second)))
			_, err = over.Read(make([]byte, 1))
			var ne net.Error
			require.False(t, errors.As(err, &ne) && ne.Timeout(), "over-cap connection was parked, not closed")

			require.NoError(t, first.Close())
			_ = first.Close() // a second close must not free another slot
			dialTCP(t, addr)
			mustAccept(t, accepted)
			require.EqualValues(t, 2, ln.open.Load())
			require.EqualValues(t, 1, ln.rejectedGlobal.Load()+ln.rejectedPerIP.Load())
		})
	}
}

// TestIdleConnectionsDoNotBlockUpload is the PROTOCO-2220 regression: more idle
// connections than the old 16-connection cap must not block a new upload.
func TestIdleConnectionsDoNotBlockUpload(t *testing.T) {
	srv, conn := startServer(t, testAdmission, &fakeFibre{})
	for range 20 {
		dialTCP(t, srv.ListenAddress())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := types.NewFibreClient(conn).UploadShard(ctx, &types.UploadShardRequest{}, grpc.WaitForReady(true))
	require.NoError(t, err)
}

func dialTCP(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func mustAccept(t *testing.T, accepted <-chan net.Conn) net.Conn {
	t.Helper()
	select {
	case c := <-accepted:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("expected the listener to accept the connection")
		return nil
	}
}
