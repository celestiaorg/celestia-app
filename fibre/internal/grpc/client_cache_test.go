package grpc_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClientCache(t *testing.T) {
	const numGoroutines = 20
	validators := []*core.Validator{
		{
			Address: []byte("validator-1"),
		},
		{
			Address: []byte("validator-2"),
		},
	}
	cache := grpc.NewClientCache(mockClientFn(false), len(validators))

	clients := make([]types.FibreClient, numGoroutines)
	errors := make([]error, numGoroutines)

	// concurrently get clients for multiple validators
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			// each goroutine requests a client from a validator (round-robin)
			val := validators[idx%len(validators)]
			clients[idx], errors[idx] = cache.GetClient(t.Context(), val)
		}(i)
	}
	wg.Wait()

	// all should succeed
	for i := range numGoroutines {
		require.NoError(t, errors[i])
		require.NotNil(t, clients[i])
	}

	// clients for the same validator should be identical
	for i := range numGoroutines {
		for j := i + 1; j < numGoroutines; j++ {
			if i%len(validators) == j%len(validators) {
				assert.Equal(t, clients[i], clients[j], "clients for same validator should be identical")
			} else {
				assert.NotEqual(t, clients[i], clients[j], "clients for different validators should be different")
			}
		}
	}

	// verify none are closed yet
	mockClient1 := clients[0].(*mockFibreClientCloser)
	mockClient2 := clients[1].(*mockFibreClientCloser)
	assert.False(t, mockClient1.closed)
	assert.False(t, mockClient2.closed)

	// close should succeed and close all clients
	err := cache.Close()
	assert.NoError(t, err)
	assert.True(t, mockClient1.closed)
	assert.True(t, mockClient2.closed)
}

// TestClientCacheGetCloseConcurrentRace verifies that concurrent calls to GetClient
// and Close do not produce a data race. Run with -race to catch the regression.
// The original sync.Once implementation allowed Close to read entry.clientCloser
// without holding the entry lock, racing with GetClient's write inside Do.
func TestClientCacheGetCloseConcurrentRace(t *testing.T) {
	const numGoroutines = 50
	cache := grpc.NewClientCache(mockClientFn(false), 1)
	val := &core.Validator{Address: []byte("validator-1")}

	var wg sync.WaitGroup
	wg.Add(numGoroutines + 1)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			cache.GetClient(t.Context(), val) //nolint:errcheck
		}()
	}

	go func() {
		defer wg.Done()
		cache.Close() //nolint:errcheck
	}()

	wg.Wait()
}

// TestClientCacheRequest_EvictsStaleClient verifies that an unreachable peer
// forces eviction: the stale client is closed and a fresh one is dialed.
func TestClientCacheRequest_EvictsStaleClient(t *testing.T) {
	cache := grpc.NewClientCache(mockClientFn(false), 1)
	val := &core.Validator{Address: []byte("validator-1")}

	var stale, fresh grpc.Client
	err := cache.Request(t.Context(), val, func(c grpc.Client) error {
		if stale == nil {
			stale = c
			return errUnreachable // unreachable peer triggers eviction + re-dial
		}
		fresh = c
		return nil // re-resolved host
	})
	require.NoError(t, err)
	assert.True(t, stale.(*mockFibreClientCloser).closed, "evicted client should be closed")
	assert.NotSame(t, stale, fresh, "a fresh client should be dialed after eviction")
}

// TestClientCacheRequest_ClearsCachedDialError verifies that eviction clears a
// cached dial error so the next call re-dials — the recovery path for a
// corrected host.
func TestClientCacheRequest_ClearsCachedDialError(t *testing.T) {
	var calls int
	fn := func(_ context.Context, val *core.Validator) (grpc.Client, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("dial failed")
		}
		return &mockFibreClientCloser{id: val.Address.String()}, nil
	}
	cache := grpc.NewClientCache(fn, 1)
	val := &core.Validator{Address: []byte("validator-1")}

	// The first two GetClient calls cache and reuse the dial error.
	_, err := cache.GetClient(t.Context(), val)
	require.Error(t, err)
	_, err = cache.GetClient(t.Context(), val)
	require.Error(t, err)
	require.Equal(t, 1, calls, "error should be cached, not re-dialed")

	// A request whose dial failed evicts the entry, clearing the cached error
	// and allowing a re-dial.
	require.NoError(t, cache.Request(t.Context(), val, func(grpc.Client) error { return nil }))
	require.Equal(t, 2, calls, "dial failure should clear the cached error and allow a re-dial")
}

// mockFibreClientCloser is a mock implementation for testing
type mockFibreClientCloser struct {
	types.FibreClient
	closed bool
	id     string // unique identifier for this client
}

func (m *mockFibreClientCloser) Close() error {
	m.closed = true
	return nil
}

// mockClientFn creates a mock grpc.NewClientFn for testing
func mockClientFn(shouldErr bool) grpc.NewClientFn {
	return func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
		if shouldErr {
			return nil, errors.New("mock client creation error")
		}
		return &mockFibreClientCloser{
			id: val.Address.String(), // use validator address as unique id
		}, nil
	}
}

// errUnreachable is a transport-level error, as a request fn would see from an
// unreachable peer.
var errUnreachable = status.Error(grpccodes.Unavailable, "rpc failed")

// requestVal is an arbitrary validator; Request tests don't depend on its identity.
var requestVal = &core.Validator{Address: []byte("v1")}

// TestClientCacheRequest_RetriesAfterUnreachable verifies an unreachable peer
// triggers exactly one re-dial and retry of fn.
func TestClientCacheRequest_RetriesAfterUnreachable(t *testing.T) {
	cache := grpc.NewClientCache(mockClientFn(false), 1)

	calls := 0
	err := cache.Request(t.Context(), requestVal, func(grpc.Client) error {
		calls++
		if calls == 1 {
			return errUnreachable // stale host
		}
		return nil // re-resolved host
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "fn should be retried once against the re-dialed client")
}

// TestClientCacheRequest_RetryFailureReturnsError verifies that when the retry
// still fails, the retry's error is returned and fn is not attempted a third
// time.
func TestClientCacheRequest_RetryFailureReturnsError(t *testing.T) {
	cache := grpc.NewClientCache(mockClientFn(false), 1)

	calls := 0
	err := cache.Request(t.Context(), requestVal, func(grpc.Client) error { calls++; return errUnreachable })
	assert.Equal(t, grpccodes.Unavailable, status.Code(err))
	assert.Equal(t, 2, calls, "fn is attempted once, then retried once")
}

// TestClientCacheRequest_AppErrorSkipsRetry verifies that an application-level
// error from a reachable server is returned as-is without a re-dial.
func TestClientCacheRequest_AppErrorSkipsRetry(t *testing.T) {
	cache := grpc.NewClientCache(mockClientFn(false), 1)

	appErr := status.Error(grpccodes.NotFound, "blob not found")
	calls := 0
	err := cache.Request(t.Context(), requestVal, func(grpc.Client) error { calls++; return appErr })
	assert.Equal(t, grpccodes.NotFound, status.Code(err))
	assert.Equal(t, 1, calls, "application errors must not trigger a re-dial")
}

// TestClientCacheRequest_DialFailureTriggersRedial verifies a failed dial (before
// any RPC) triggers a re-dial.
func TestClientCacheRequest_DialFailureTriggersRedial(t *testing.T) {
	dials := 0
	dial := func(context.Context, *core.Validator) (grpc.Client, error) {
		dials++
		if dials == 1 {
			return nil, errors.New("invalid host") // dial fails before any RPC
		}
		return &mockFibreClientCloser{}, nil
	}
	cache := grpc.NewClientCache(dial, 1)

	calls := 0
	err := cache.Request(t.Context(), requestVal, func(grpc.Client) error { calls++; return nil })
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "fn runs once, against the re-dialed client")
	assert.Equal(t, 2, dials, "the failed dial should be retried once")
}

// TestClientCacheRequest_SkipsRetryOnCancelledContext verifies a cancelled
// context short-circuits the retry path.
func TestClientCacheRequest_SkipsRetryOnCancelledContext(t *testing.T) {
	cache := grpc.NewClientCache(mockClientFn(false), 1)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	calls := 0
	err := cache.Request(ctx, requestVal, func(grpc.Client) error { calls++; return errUnreachable })
	require.Error(t, err)
	assert.Equal(t, 1, calls, "a cancelled context must not trigger a re-dial")
}
