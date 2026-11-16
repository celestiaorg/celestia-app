package grpc

import (
	"context"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/fibre/validator"
	"github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

type staticHostRegistry validator.Host

func (h staticHostRegistry) GetHost(context.Context, *core.Validator) (validator.Host, error) {
	return validator.Host(h), nil
}

// acceptedWithin dials host through [DefaultNewClientFn] and reports whether
// lis accepted a connection within timeout.
func acceptedWithin(t *testing.T, lis net.Listener, host string, timeout time.Duration) bool {
	t.Helper()
	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
		accepted <- struct{}{}
	}()

	pub := ed25519.GenPrivKey().PubKey()
	val := &core.Validator{Address: pub.Address(), PubKey: pub}
	newClient := DefaultNewClientFn(staticHostRegistry(host), func() string { return "test-chain" }, 1<<20, nil)
	client, err := newClient(t.Context(), val)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	client.(*fibreClientCloser).Connect()

	select {
	case <-accepted:
		return true
	case <-time.After(timeout):
		return false
	}
}

// TestDefaultNewClientFnDialsNetworkHost checks that a registered host which
// looks like a gRPC resolver scheme is not dialed as a local unix socket.
func TestDefaultNewClientFnDialsNetworkHost(t *testing.T) {
	t.Run("unix scheme is not a local socket", func(t *testing.T) {
		t.Chdir(t.TempDir())
		lis, err := net.Listen("unix", "9090")
		require.NoError(t, err)
		t.Cleanup(func() { _ = lis.Close() })

		require.False(t, acceptedWithin(t, lis, "unix:9090", time.Second),
			"client dialed a local unix socket for host unix:9090")
	})

	t.Run("unix-abstract scheme is not a local socket", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("abstract unix sockets are Linux-only")
		}
		lis, err := net.Listen("unix", "@9090")
		require.NoError(t, err)
		t.Cleanup(func() { _ = lis.Close() })

		require.False(t, acceptedWithin(t, lis, "unix-abstract:9090", time.Second),
			"client dialed a local abstract socket for host unix-abstract:9090")
	})

	t.Run("host:port still dials over TCP", func(t *testing.T) {
		lis, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = lis.Close() })

		require.True(t, acceptedWithin(t, lis, lis.Addr().String(), 5*time.Second))
	})

	t.Run("[ipv6]:port still dials over TCP", func(t *testing.T) {
		lis, err := net.Listen("tcp", "[::1]:0")
		if err != nil {
			t.Skipf("IPv6 loopback unavailable: %v", err)
		}
		t.Cleanup(func() { _ = lis.Close() })

		require.True(t, acceptedWithin(t, lis, lis.Addr().String(), 5*time.Second))
	})
}
