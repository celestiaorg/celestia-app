package grpc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// rotatingMockClient is a minimal [Client] for the rotation tests: it never
// performs a real RPC, so rotation is driven purely by the connection age.
type rotatingMockClient struct {
	types.FibreClient
	closed atomic.Bool
}

func (c *rotatingMockClient) Close() error {
	c.closed.Store(true)
	return nil
}

// readyWaiterMockClient is a [rotatingMockClient] that also reports connection
// readiness through WaitReady, so a rotation can be driven into its
// "replacement not ready" branch.
type readyWaiterMockClient struct {
	rotatingMockClient
	ready atomic.Bool
}

func (c *readyWaiterMockClient) WaitReady(context.Context) bool { return c.ready.Load() }

// TestClientCacheRotate_ReplacementNotReady checks the rotation branch where a
// replacement implements the ready-waiter interface and reports not-ready: the
// replacement is closed, the old connection stays in service, and the rotation
// goroutine finishes so Close returns without leaking.
func TestClientCacheRotate_ReplacementNotReady(t *testing.T) {
	oldAge, oldGrace := keepAliveMaxConnAge, keepAliveMaxConnAgeGrace
	keepAliveMaxConnAge, keepAliveMaxConnAgeGrace = time.Millisecond, time.Millisecond
	t.Cleanup(func() { keepAliveMaxConnAge, keepAliveMaxConnAgeGrace = oldAge, oldGrace })

	old := &rotatingMockClient{}
	replacement := &readyWaiterMockClient{} // ready is false, so WaitReady fails

	var dials atomic.Int32
	cache := NewClientCache(func(context.Context, *core.Validator) (Client, error) {
		if dials.Add(1) == 1 {
			return old, nil // the initial client
		}
		return replacement, nil // every replacement reports not-ready
	}, 1)
	t.Cleanup(func() { _ = cache.Close() })

	val := &core.Validator{Address: []byte("v1")}
	// First request dials the old client.
	require.NoError(t, cache.Request(t.Context(), val, func(Client) error { return nil }))
	time.Sleep(5 * time.Millisecond) // age it past rotateAge

	// Second request triggers the background rotation; the request keeps
	// serving on the old client while the replacement is dialed.
	require.NoError(t, cache.Request(t.Context(), val, func(c Client) error {
		require.Same(t, old, c, "the old client serves while rotating")
		return nil
	}))

	// The replacement's readiness probe fails, so it is closed and never
	// swapped in. (closed can only be set here; the replacement is never
	// cached, so nothing else would close it before cache.Close.)
	require.Eventually(t, replacement.closed.Load, 5*time.Second, 5*time.Millisecond,
		"a not-ready replacement must be closed")
	require.False(t, old.closed.Load(), "the old client must keep serving")

	// Stop further rotations, then confirm the old client still serves.
	keepAliveMaxConnAge = time.Hour
	require.NoError(t, cache.Request(t.Context(), val, func(c Client) error {
		require.Same(t, old, c, "the old client still serves after a failed rotation")
		return nil
	}))

	// No goroutine leaks: the failed rotation's goroutine has finished, so
	// Close cancels/joins it and returns promptly.
	done := make(chan error, 1)
	go func() { done <- cache.Close() }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return; rotation goroutine leaked")
	}
	require.True(t, old.closed.Load(), "Close must close the old client")
}

// TestClientCacheRotatesBeforeMaxAge checks that cached clients are replaced
// before the server's connection age cap would close them, without failed
// requests. It drives rotation with a mock client so it needs no real
// transport.
func TestClientCacheRotatesBeforeMaxAge(t *testing.T) {
	oldAge, oldGrace := keepAliveMaxConnAge, keepAliveMaxConnAgeGrace
	keepAliveMaxConnAge, keepAliveMaxConnAgeGrace = 100*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { keepAliveMaxConnAge, keepAliveMaxConnAgeGrace = oldAge, oldGrace })

	var dials atomic.Int32
	cache := NewClientCache(func(context.Context, *core.Validator) (Client, error) {
		dials.Add(1)
		return &rotatingMockClient{}, nil
	}, 1)
	t.Cleanup(func() { _ = cache.Close() })

	val := &core.Validator{Address: []byte("v1")}
	for end := time.Now().Add(time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		err := cache.Request(t.Context(), val, func(Client) error { return nil })
		require.NoError(t, err)
	}
	require.GreaterOrEqual(t, dials.Load(), int32(3), "expected a replacement about every 80ms")
}

// TestClientCacheClose_CancelsInFlightRotation checks that Close cancels a
// background rotation that is still dialing and waits for its goroutine to
// finish, so a replacement connection never outlives the cache.
func TestClientCacheClose_CancelsInFlightRotation(t *testing.T) {
	oldAge, oldGrace := keepAliveMaxConnAge, keepAliveMaxConnAgeGrace
	keepAliveMaxConnAge, keepAliveMaxConnAgeGrace = time.Millisecond, time.Millisecond
	t.Cleanup(func() { keepAliveMaxConnAge, keepAliveMaxConnAgeGrace = oldAge, oldGrace })

	var dials atomic.Int32
	dialing := make(chan struct{})
	cache := NewClientCache(func(ctx context.Context, _ *core.Validator) (Client, error) {
		if dials.Add(1) == 1 {
			return &rotatingMockClient{}, nil // the initial client
		}
		// The replacement dial blocks until Close cancels the rotation.
		close(dialing)
		<-ctx.Done()
		return nil, ctx.Err()
	}, 1)

	val := &core.Validator{Address: []byte("v1")}
	require.NoError(t, cache.Request(t.Context(), val, func(Client) error { return nil }))
	time.Sleep(5 * time.Millisecond) // age the client past rotateAge
	require.NoError(t, cache.Request(t.Context(), val, func(Client) error { return nil }))
	<-dialing // the replacement dial is now in flight

	done := make(chan error, 1)
	go func() { done <- cache.Close() }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return; in-flight rotation was not cancelled")
	}
}
