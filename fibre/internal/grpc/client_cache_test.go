package grpc_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
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
	cache := grpc.NewClientCache(mockClientFn(false), stubHostRegistry{}, len(validators))

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
	cache := grpc.NewClientCache(mockClientFn(false), stubHostRegistry{}, 1)
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

func TestClientCacheEvict(t *testing.T) {
	cache := grpc.NewClientCache(mockClientFn(false), stubHostRegistry{}, 1)
	val := &core.Validator{Address: []byte("validator-1")}

	c1, err := cache.GetClient(t.Context(), val)
	require.NoError(t, err)
	require.NotNil(t, c1)

	cache.Evict(val)
	assert.True(t, c1.(*mockFibreClientCloser).closed, "evicted client should be closed immediately")

	c2, err := cache.GetClient(t.Context(), val)
	require.NoError(t, err)
	assert.NotSame(t, c1, c2, "GetClient after Evict should re-dial a new client")

	// Evicting an unknown validator is a no-op.
	cache.Evict(&core.Validator{Address: []byte("unknown")})
}

// TestClientCacheEvictClearsCachedError verifies Evict clears a cached dial
// error so the next GetClient retries — the recovery path for a corrected host.
func TestClientCacheEvictClearsCachedError(t *testing.T) {
	var calls int
	fn := func(_ context.Context, val *core.Validator) (grpc.Client, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("dial failed")
		}
		return &mockFibreClientCloser{id: val.Address.String()}, nil
	}
	cache := grpc.NewClientCache(fn, stubHostRegistry{}, 1)
	val := &core.Validator{Address: []byte("validator-1")}

	_, err := cache.GetClient(t.Context(), val)
	require.Error(t, err)
	_, err = cache.GetClient(t.Context(), val)
	require.Error(t, err)
	require.Equal(t, 1, calls, "error should be cached, not re-dialed")

	cache.Evict(val)
	c, err := cache.GetClient(t.Context(), val)
	require.NoError(t, err)
	require.NotNil(t, c)
	require.Equal(t, 2, calls, "Evict should clear the cached error and allow a re-dial")
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

// stubHostRegistry is a configurable validator.HostRegistry for cache tests.
// refresh, when set, backs RefreshHost; otherwise RefreshHost reports no change.
type stubHostRegistry struct {
	refresh func() (changed bool, valid bool, err error)
}

func (stubHostRegistry) GetHost(context.Context, *core.Validator) (validator.Host, error) {
	return "", nil
}

func (s stubHostRegistry) RefreshHost(context.Context, *core.Validator) (bool, bool, error) {
	if s.refresh != nil {
		return s.refresh()
	}
	return false, false, nil
}

// hostClient is a fake grpc.Client bound to the host it was dialed against.
type hostClient struct {
	types.FibreClient
	host string
}

func (hostClient) Close() error { return nil }

// hostClientFn dials a hostClient bound to the current value of *host.
func hostClientFn(host *string, mu *sync.Mutex) grpc.NewClientFn {
	return func(context.Context, *core.Validator) (grpc.Client, error) {
		mu.Lock()
		defer mu.Unlock()
		return &hostClient{host: *host}, nil
	}
}

// unreachableUnlessGood returns a request fn that succeeds (recording the host
// into got) only when the client is bound to "good", otherwise returns a
// transport (Unavailable) error like an unreachable peer.
func unreachableUnlessGood(got *string) func(grpc.Client) error {
	return func(cl grpc.Client) error {
		h := cl.(*hostClient).host
		if h != "good" {
			return status.Error(grpccodes.Unavailable, "rpc failed")
		}
		*got = h
		return nil
	}
}

func TestClientCacheRequest_RetriesAfterHostChange(t *testing.T) {
	var mu sync.Mutex
	host := "bad"
	reg := stubHostRegistry{refresh: func() (bool, bool, error) {
		mu.Lock()
		host = "good" // host corrected on chain
		mu.Unlock()
		return true, true, nil
	}}
	cache := grpc.NewClientCache(hostClientFn(&host, &mu), reg, 1)

	var got string
	err := cache.Request(t.Context(), &core.Validator{Address: []byte("v1")}, unreachableUnlessGood(&got))
	require.NoError(t, err)
	assert.Equal(t, "good", got)
}

func TestClientCacheRequest_UnchangedReturnsOriginalError(t *testing.T) {
	var mu sync.Mutex
	host := "bad"
	reg := stubHostRegistry{refresh: func() (bool, bool, error) { return false, false, nil }}
	cache := grpc.NewClientCache(hostClientFn(&host, &mu), reg, 1)

	var got string
	err := cache.Request(t.Context(), &core.Validator{Address: []byte("v1")}, unreachableUnlessGood(&got))
	require.Error(t, err)
	assert.Equal(t, grpccodes.Unavailable, status.Code(err))
}

func TestClientCacheRequest_ChangedButInvalidReturnsOriginalError(t *testing.T) {
	var mu sync.Mutex
	host := "bad"
	refreshed := false
	reg := stubHostRegistry{refresh: func() (bool, bool, error) {
		refreshed = true
		mu.Lock()
		host = "still-bad" // changed, but the new host is invalid
		mu.Unlock()
		return true, false, nil
	}}
	cache := grpc.NewClientCache(hostClientFn(&host, &mu), reg, 1)

	var got string
	err := cache.Request(t.Context(), &core.Validator{Address: []byte("v1")}, unreachableUnlessGood(&got))
	require.Error(t, err)
	assert.Equal(t, grpccodes.Unavailable, status.Code(err), "should not retry into a known-invalid host")
	assert.True(t, refreshed)
}

func TestClientCacheRequest_AppErrorSkipsRefresh(t *testing.T) {
	var mu sync.Mutex
	host := "good" // dial + connection succeed
	refreshed := false
	reg := stubHostRegistry{refresh: func() (bool, bool, error) {
		refreshed = true
		return true, true, nil
	}}
	cache := grpc.NewClientCache(hostClientFn(&host, &mu), reg, 1)

	appErr := status.Error(grpccodes.NotFound, "blob not found")
	err := cache.Request(t.Context(), &core.Validator{Address: []byte("v1")},
		func(grpc.Client) error { return appErr })
	require.Error(t, err)
	assert.Equal(t, grpccodes.NotFound, status.Code(err))
	assert.False(t, refreshed, "application errors must not trigger a host refresh")
}

func TestClientCacheRequest_DialFailureTriggersRefresh(t *testing.T) {
	var mu sync.Mutex
	host := "bad"
	dial := func(context.Context, *core.Validator) (grpc.Client, error) {
		mu.Lock()
		defer mu.Unlock()
		if host == "good" {
			return &hostClient{host: host}, nil
		}
		return nil, errors.New("invalid host") // plain error, not a gRPC status
	}
	reg := stubHostRegistry{refresh: func() (bool, bool, error) {
		mu.Lock()
		host = "good"
		mu.Unlock()
		return true, true, nil
	}}
	cache := grpc.NewClientCache(dial, reg, 1)

	var got string
	err := cache.Request(t.Context(), &core.Validator{Address: []byte("v1")}, unreachableUnlessGood(&got))
	require.NoError(t, err)
	assert.Equal(t, "good", got)
}

func TestClientCacheRequest_SkipsRefreshOnCancelledContext(t *testing.T) {
	var mu sync.Mutex
	host := "bad"
	refreshed := false
	reg := stubHostRegistry{refresh: func() (bool, bool, error) {
		refreshed = true
		return true, true, nil
	}}
	cache := grpc.NewClientCache(hostClientFn(&host, &mu), reg, 1)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var got string
	err := cache.Request(ctx, &core.Validator{Address: []byte("v1")}, unreachableUnlessGood(&got))
	require.Error(t, err)
	assert.False(t, refreshed, "a cancelled context must not trigger a refresh")
}
