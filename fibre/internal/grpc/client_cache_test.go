package grpc_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
			errors[idx] = cache.Request(t.Context(), val, func(client grpc.Client) error {
				clients[idx] = client
				return nil
			})
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

// TestClientCacheRequestCloseConcurrentRace verifies that concurrent calls to Request
// and Close do not produce a data race. Run with -race to catch the regression.
func TestClientCacheRequestCloseConcurrentRace(t *testing.T) {
	const numGoroutines = 50
	cache := grpc.NewClientCache(mockClientFn(false), 1)
	val := &core.Validator{Address: []byte("validator-1")}

	var wg sync.WaitGroup
	wg.Add(numGoroutines + 1)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			cache.Request(t.Context(), val, func(grpc.Client) error { return nil }) //nolint:errcheck
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

	// A cancelled context prevents retries, leaving the dial error cached.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := cache.Request(ctx, val, func(grpc.Client) error { return nil })
	require.Error(t, err)
	err = cache.Request(ctx, val, func(grpc.Client) error { return nil })
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

// lifetimeClient signals closure so requests can detect premature cancellation.
type lifetimeClient struct {
	types.FibreClient
	closed chan struct{}
	closes atomic.Int32
}

func (c *lifetimeClient) Close() error {
	if c.closes.Add(1) == 1 {
		close(c.closed)
	}
	return nil
}

func TestClientCacheRequest_RetiresActiveConnection(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "release"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			cache := grpc.NewClientCache(func(context.Context, *core.Validator) (grpc.Client, error) {
				return &lifetimeClient{closed: make(chan struct{})}, nil
			}, 1)
			defer cache.Close() //nolint:errcheck
			active := make(chan *lifetimeClient, 1)
			finish := make(chan struct{})
			defer close(finish)
			done := make(chan error, 1)
			go func() {
				done <- cache.Request(t.Context(), requestVal, func(c grpc.Client) error {
					active <- c.(*lifetimeClient)
					select {
					case <-finish:
						return nil
					case <-c.(*lifetimeClient).closed:
						return status.Error(grpccodes.Canceled, "connection closed")
					}
				})
			}()
			stale := <-active
			var fresh *lifetimeClient
			require.NoError(t, cache.Request(t.Context(), requestVal, func(c grpc.Client) error {
				if c == stale {
					return status.Error(grpccodes.DeadlineExceeded, "timeout")
				}
				fresh = c.(*lifetimeClient)
				return nil
			}))
			require.Zero(t, stale.closes.Load(), "eviction must not abort another request")
			if shutdown {
				require.NoError(t, cache.Close())
				require.Equal(t, grpccodes.Canceled, status.Code(<-done))
				require.EqualValues(t, 1, fresh.closes.Load())
			} else {
				finish <- struct{}{}
				require.NoError(t, <-done)
				require.Zero(t, fresh.closes.Load())
			}
			require.EqualValues(t, 1, stale.closes.Load())
		})
	}
}

func TestClientCacheRequest_OldFailureKeepsReplacement(t *testing.T) {
	var dials atomic.Int32
	cache := grpc.NewClientCache(func(context.Context, *core.Validator) (grpc.Client, error) {
		dials.Add(1)
		return &lifetimeClient{closed: make(chan struct{})}, nil
	}, 1)
	defer cache.Close() //nolint:errcheck
	active := make(chan grpc.Client, 1)
	fail := make(chan struct{})
	defer close(fail)
	done := make(chan error, 1)
	var retried grpc.Client
	go func() {
		attempts := 0
		done <- cache.Request(t.Context(), requestVal, func(c grpc.Client) error {
			attempts++
			if attempts == 1 {
				active <- c
				<-fail
				return errUnreachable
			}
			retried = c
			return nil
		})
	}()
	stale := <-active
	var replacement grpc.Client
	require.NoError(t, cache.Request(t.Context(), requestVal, func(c grpc.Client) error {
		if c == stale {
			return errUnreachable
		}
		replacement = c
		return nil
	}))
	fail <- struct{}{}
	require.NoError(t, <-done)
	require.Same(t, replacement, retried)
	require.EqualValues(t, 2, dials.Load())
	require.Zero(t, replacement.(*lifetimeClient).closes.Load())
}

type blockingCloseClient struct {
	lifetimeClient
	started chan struct{}
	finish  chan struct{}
}

func (c *blockingCloseClient) Close() error {
	close(c.started)
	<-c.finish
	return c.lifetimeClient.Close()
}

func TestClientCacheClose_WaitsForConcurrentClose(t *testing.T) {
	client := &blockingCloseClient{
		lifetimeClient: lifetimeClient{closed: make(chan struct{})},
		started:        make(chan struct{}),
		finish:         make(chan struct{}),
	}
	cache := grpc.NewClientCache(func(context.Context, *core.Validator) (grpc.Client, error) {
		return client, nil
	}, 1)
	require.NoError(t, cache.Request(t.Context(), requestVal, func(grpc.Client) error { return nil }))
	firstDone := make(chan error, 1)
	go func() { firstDone <- cache.Close() }()
	<-client.started
	secondDone := make(chan struct{})
	go func() {
		assert.NoError(t, cache.Close())
		close(secondDone)
	}()
	select {
	case <-secondDone:
		t.Error("concurrent shutdown returned before the connection closed")
	case <-time.After(100 * time.Millisecond):
	}
	close(client.finish)
	require.NoError(t, <-firstDone)
	<-secondDone
	require.EqualValues(t, 1, client.closes.Load())
}

func TestClientCacheClose_WaitsForRetiredConnection(t *testing.T) {
	for _, retireWithActiveRequest := range []bool{false, true} {
		name := "evict"
		if retireWithActiveRequest {
			name = "release"
		}
		t.Run(name, func(t *testing.T) {
			stale := &blockingCloseClient{
				lifetimeClient: lifetimeClient{closed: make(chan struct{})},
				started:        make(chan struct{}),
				finish:         make(chan struct{}),
			}
			dials := 0
			cache := grpc.NewClientCache(func(context.Context, *core.Validator) (grpc.Client, error) {
				dials++
				if dials == 1 {
					return stale, nil
				}
				return &lifetimeClient{closed: make(chan struct{})}, nil
			}, 1)
			defer cache.Close() //nolint:errcheck
			requestDone := make(chan error, 1)
			failStale := func(c grpc.Client) error {
				if c == stale {
					return errUnreachable
				}
				return nil
			}
			if retireWithActiveRequest {
				active := make(chan struct{})
				finishRequest := make(chan struct{})
				go func() {
					requestDone <- cache.Request(t.Context(), requestVal, func(grpc.Client) error {
						close(active)
						<-finishRequest
						return nil
					})
				}()
				<-active
				require.NoError(t, cache.Request(t.Context(), requestVal, failStale))
				close(finishRequest)
			} else {
				go func() { requestDone <- cache.Request(t.Context(), requestVal, failStale) }()
			}
			<-stale.started
			shutdownDone := make(chan struct{})
			go func() {
				assert.NoError(t, cache.Close())
				close(shutdownDone)
			}()
			select {
			case <-shutdownDone:
				t.Error("cache shutdown returned before the retired connection closed")
			case <-time.After(100 * time.Millisecond):
			}
			close(stale.finish)
			<-shutdownDone
			<-requestDone
			require.EqualValues(t, 1, stale.closes.Load())
		})
	}
}
