package grpc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestConnLimiter(t *testing.T) {
	t.Run("global cap", func(t *testing.T) {
		cl := newConnLimiter(2, 0)
		require.True(t, cl.acquire("a"))
		require.True(t, cl.acquire("b"))
		require.False(t, cl.acquire("c")) // over the global cap
		require.Equal(t, int64(1), cl.rejectedGlobal.Load())
		cl.release("a")
		require.True(t, cl.acquire("c")) // slot freed
	})
	t.Run("per-IP cap", func(t *testing.T) {
		cl := newConnLimiter(0, 2)
		require.True(t, cl.acquire("1.1.1.1"))
		require.True(t, cl.acquire("1.1.1.1"))
		require.False(t, cl.acquire("1.1.1.1")) // third from the same IP
		require.True(t, cl.acquire("2.2.2.2"))  // a different IP is fine
		require.Equal(t, int64(1), cl.rejectedPerIP.Load())
		cl.release("1.1.1.1")
		require.True(t, cl.acquire("1.1.1.1"))
	})
	t.Run("disabled", func(t *testing.T) {
		cl := newConnLimiter(0, 0)
		for range 100 {
			require.True(t, cl.acquire("x"))
		}
	})
}

// TestConnLimiterFailFast: an over-cap connection is accepted then closed fast
// (not parked), and the slot frees on close.
func TestConnLimiterFailFast(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	limiter := newConnLimiter(2, 0)
	ln := newLimitedListener(inner, limiter)
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
	sc1 := mustAccept(t, accepted)
	dialTCP(t, addr)
	mustAccept(t, accepted)

	// Third exceeds the cap: closed fast, not admitted.
	c3 := dialTCP(t, addr)
	require.NoError(t, c3.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, err = c3.Read(make([]byte, 1))
	require.Error(t, err)
	var ne net.Error
	require.False(t, errors.As(err, &ne) && ne.Timeout(), "connection was parked, not closed fast")
	require.Eventually(t, func() bool { return limiter.rejectedGlobal.Load() == 1 }, 2*time.Second, 5*time.Millisecond)

	// Closing an admitted conn frees a slot.
	require.NoError(t, sc1.Close())
	dialTCP(t, addr)
	mustAccept(t, accepted)
}

// TestIdleConnectionsDoNotBlockUpload is the PROTOCO-2220 regression: more than the
// old 16-connection cap of idle connections must not block a new upload (the global
// cap is now 4096 and fail-fast).
func TestIdleConnectionsDoNotBlockUpload(t *testing.T) {
	srv := startAdmission(t, 1<<20, 1<<16, &fakeFibre{}) // per-IP disabled
	addr := srv.ListenAddress()
	for range 20 {
		dialTCP(t, addr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := dial(t, addr).UploadShard(ctx, &types.UploadShardRequest{}, grpc.WaitForReady(true))
	require.NoError(t, err)
	require.Equal(t, []byte("ok"), resp.ValidatorSignature)
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
		t.Fatal("expected the listener to admit the connection")
		return nil
	}
}
