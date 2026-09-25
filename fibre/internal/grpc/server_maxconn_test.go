package grpc

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestListenCapsConnectionsAtConfiguredLimit checks that the listener accepts at
// most maxConnections connections, accepts more only once a slot frees, and that
// the cap follows the configured value.
func TestListenCapsConnectionsAtConfiguredLimit(t *testing.T) {
	for _, maxConns := range []int{2, 4} {
		t.Run("", func(t *testing.T) {
			srv, err := Listen("127.0.0.1:0", maxConns, DefaultMaxConcurrentStreams)
			require.NoError(t, err)
			defer srv.listener.Close()

			addr := srv.ListenAddress()

			accepted := make(chan net.Conn, maxConns+1)
			go func() {
				for {
					c, err := srv.listener.Accept()
					if err != nil {
						return
					}
					accepted <- c
				}
			}()

			held := make([]net.Conn, 0, maxConns)
			for range maxConns {
				c, err := net.Dial("tcp", addr)
				require.NoError(t, err)
				defer c.Close()
				held = append(held, mustAccept(t, accepted))
			}

			extra, err := net.Dial("tcp", addr)
			require.NoError(t, err)
			defer extra.Close()
			select {
			case <-accepted:
				t.Fatal("listener accepted a connection beyond maxConnections")
			case <-time.After(200 * time.Millisecond):
			}

			require.NoError(t, held[0].Close())
			mustAccept(t, accepted)
		})
	}
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
