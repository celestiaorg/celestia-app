package fibre

import (
	"context"
	"errors"
	"sync"
	"testing"

	fibregrpc "github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v9/fibre/state"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeFibreClient is a gRPC client bound to the host it was dialed against.
// Its do() succeeds only when bound to the "good" host.
type fakeFibreClient struct {
	types.FibreClient
	host   string
	closed bool
}

func (f *fakeFibreClient) Close() error { f.closed = true; return nil }

// fakeState implements state.Client but only RefreshHost is exercised.
type fakeState struct {
	state.Client
	refresh func() (bool, bool, error)
}

func (s *fakeState) RefreshHost(context.Context, *core.Validator) (bool, bool, error) {
	return s.refresh()
}

// newRetryClient builds a minimal Client whose ClientCache dials a
// fakeFibreClient bound to the current value of host.
func newRetryClient(host *string, mu *sync.Mutex, refresh func() (bool, bool, error)) *Client {
	fn := func(_ context.Context, _ *core.Validator) (fibregrpc.Client, error) {
		mu.Lock()
		defer mu.Unlock()
		return &fakeFibreClient{host: *host}, nil
	}
	return &Client{
		state:       &fakeState{refresh: refresh},
		clientCache: fibregrpc.NewClientCache(fn, 1),
	}
}

// do returns the bound host, or a transport (Unavailable) error when bound to
// the "bad" host — mimicking an unreachable peer.
func do(cl fibregrpc.Client) (string, error) {
	f := cl.(*fakeFibreClient)
	if f.host != "good" {
		return "", status.Error(grpccodes.Unavailable, "rpc failed")
	}
	return f.host, nil
}

func TestWithHostRefresh_RetriesAfterHostChange(t *testing.T) {
	var mu sync.Mutex
	host := "bad"
	c := newRetryClient(&host, &mu, func() (bool, bool, error) {
		mu.Lock()
		host = "good" // host corrected on chain
		mu.Unlock()
		return true, true, nil
	})

	res, err := withHostRefresh(c, t.Context(), &core.Validator{Address: []byte("v1")}, do)
	require.NoError(t, err)
	assert.Equal(t, "good", res)
}

func TestWithHostRefresh_UnchangedReturnsOriginalError(t *testing.T) {
	var mu sync.Mutex
	host := "bad"
	c := newRetryClient(&host, &mu, func() (bool, bool, error) {
		return false, false, nil // host did not change
	})

	_, err := withHostRefresh(c, t.Context(), &core.Validator{Address: []byte("v1")}, do)
	require.Error(t, err)
	assert.Equal(t, grpccodes.Unavailable, status.Code(err))
}

func TestWithHostRefresh_ChangedButInvalidReturnsOriginalError(t *testing.T) {
	var mu sync.Mutex
	host := "bad"
	refreshed := false
	c := newRetryClient(&host, &mu, func() (bool, bool, error) {
		refreshed = true
		host = "still-bad" // changed, but the new host is invalid
		return true, false, nil
	})

	_, err := withHostRefresh(c, t.Context(), &core.Validator{Address: []byte("v1")}, do)
	require.Error(t, err)
	assert.Equal(t, grpccodes.Unavailable, status.Code(err), "should not retry into a known-invalid host")
	assert.True(t, refreshed)
}

// TestWithHostRefresh_AppErrorSkipsRefresh verifies an application-level error
// from a reachable peer (e.g. NotFound) does not trigger a host refresh.
func TestWithHostRefresh_AppErrorSkipsRefresh(t *testing.T) {
	var mu sync.Mutex
	host := "good" // dial + connection succeed
	refreshed := false
	c := newRetryClient(&host, &mu, func() (bool, bool, error) {
		refreshed = true
		return true, true, nil
	})

	appErr := status.Error(grpccodes.NotFound, "blob not found")
	_, err := withHostRefresh(c, t.Context(), &core.Validator{Address: []byte("v1")},
		func(fibregrpc.Client) (string, error) { return "", appErr })
	require.Error(t, err)
	assert.Equal(t, grpccodes.NotFound, status.Code(err))
	assert.False(t, refreshed, "application errors must not trigger a host refresh")
}

// TestWithHostRefresh_DialFailureTriggersRefresh verifies that a failed dial
// (e.g. an invalid host that returns no gRPC status) is itself enough to
// trigger a refresh, recovering once the host is corrected.
func TestWithHostRefresh_DialFailureTriggersRefresh(t *testing.T) {
	var mu sync.Mutex
	host := "bad"
	dial := func(_ context.Context, _ *core.Validator) (fibregrpc.Client, error) {
		mu.Lock()
		defer mu.Unlock()
		if host == "good" {
			return &fakeFibreClient{host: host}, nil
		}
		return nil, errors.New("invalid host") // plain error, not a gRPC status
	}
	c := &Client{
		state: &fakeState{refresh: func() (bool, bool, error) {
			mu.Lock()
			host = "good"
			mu.Unlock()
			return true, true, nil
		}},
		clientCache: fibregrpc.NewClientCache(dial, 1),
	}

	res, err := withHostRefresh(c, t.Context(), &core.Validator{Address: []byte("v1")}, do)
	require.NoError(t, err)
	assert.Equal(t, "good", res)
}

func TestWithHostRefresh_SkipsRefreshOnCancelledContext(t *testing.T) {
	var mu sync.Mutex
	host := "bad"
	refreshed := false
	c := newRetryClient(&host, &mu, func() (bool, bool, error) {
		refreshed = true
		return true, true, nil
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := withHostRefresh(c, ctx, &core.Validator{Address: []byte("v1")}, do)
	require.Error(t, err)
	assert.False(t, refreshed, "a cancelled context must not trigger a refresh")
}
