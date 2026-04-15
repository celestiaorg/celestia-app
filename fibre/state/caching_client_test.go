package state_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre/state"
	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	core "github.com/cometbft/cometbft/types"
)

// mockClient tracks calls to Head and increments height on each fetch.
type mockClient struct {
	headCalls atomic.Int64
	height    atomic.Uint64
}

func newMockClient(startHeight uint64) *mockClient {
	m := &mockClient{}
	m.height.Store(startHeight)
	return m
}

func (m *mockClient) Head(context.Context) (validator.Set, error) {
	m.headCalls.Add(1)
	h := m.height.Add(1)
	return validator.Set{ValidatorSet: &core.ValidatorSet{}, Height: h}, nil
}

func (m *mockClient) GetByHeight(_ context.Context, h uint64) (validator.Set, error) {
	m.headCalls.Add(1)
	return validator.Set{ValidatorSet: &core.ValidatorSet{}, Height: h}, nil
}

func (m *mockClient) GetHost(_ context.Context, _ *core.Validator) (validator.Host, error) {
	return "", nil
}

func (m *mockClient) ChainID() string { return "test" }

func (m *mockClient) VerifyPromise(context.Context, *state.PaymentPromise) (state.VerifiedPromise, error) {
	return state.VerifiedPromise{}, nil
}

func (m *mockClient) Start(context.Context) error { return nil }
func (m *mockClient) Stop(context.Context) error  { return nil }

func TestCachingClient_HeadCachesWithinTTL(t *testing.T) {
	inner := newMockClient(0)
	client := state.NewCachingClient(inner, time.Minute)
	ctx := context.Background()

	// First call fetches.
	set1, err := client.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Subsequent calls within TTL return cached result.
	for range 5 {
		set, err := client.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if set.Height != set1.Height {
			t.Fatalf("expected cached height %d, got %d", set1.Height, set.Height)
		}
	}

	if calls := inner.headCalls.Load(); calls != 1 {
		t.Fatalf("expected 1 inner call, got %d", calls)
	}
}

func TestCachingClient_HeadRefreshesAfterTTL(t *testing.T) {
	inner := newMockClient(0)
	// Use a tiny TTL so it expires immediately.
	client := state.NewCachingClient(inner, time.Nanosecond)
	ctx := context.Background()

	set1, err := client.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Sleep past the TTL.
	time.Sleep(time.Millisecond)

	set2, err := client.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if set2.Height == set1.Height {
		t.Fatal("expected height to advance after TTL expiry")
	}
	if calls := inner.headCalls.Load(); calls != 2 {
		t.Fatalf("expected 2 inner calls, got %d", calls)
	}
}

func TestCachingClient_ZeroTTLNeverRefreshes(t *testing.T) {
	inner := newMockClient(0)
	client := state.NewCachingClient(inner, 0)
	ctx := context.Background()

	set1, err := client.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for range 10 {
		set, err := client.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if set.Height != set1.Height {
			t.Fatalf("expected constant height %d, got %d", set1.Height, set.Height)
		}
	}

	if calls := inner.headCalls.Load(); calls != 1 {
		t.Fatalf("expected 1 inner call with TTL=0, got %d", calls)
	}
}

func TestCachingClient_GetByHeightReturnsCachedValset(t *testing.T) {
	inner := newMockClient(0)
	client := state.NewCachingClient(inner, time.Minute)
	ctx := context.Background()

	// Seed the cache via Head.
	_, err := client.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// GetByHeight should return cached validators with the requested height.
	for _, h := range []uint64{1, 100, 999} {
		set, err := client.GetByHeight(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if set.Height != h {
			t.Fatalf("GetByHeight(%d): expected height %d, got %d", h, h, set.Height)
		}
	}

	// Only the initial Head call should have hit the inner client.
	if calls := inner.headCalls.Load(); calls != 1 {
		t.Fatalf("expected 1 inner call, got %d", calls)
	}
}

func TestCachingClient_GetByHeightSeedsCacheIfEmpty(t *testing.T) {
	inner := newMockClient(0)
	client := state.NewCachingClient(inner, time.Minute)
	ctx := context.Background()

	// GetByHeight without a prior Head should seed the cache.
	set, err := client.GetByHeight(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if set.Height != 42 {
		t.Fatalf("expected height 42, got %d", set.Height)
	}
	if calls := inner.headCalls.Load(); calls != 1 {
		t.Fatalf("expected 1 inner call to seed cache, got %d", calls)
	}
}

func TestCachingClient_DelegatesOtherMethods(t *testing.T) {
	inner := newMockClient(0)
	var client state.Client = state.NewCachingClient(inner, time.Minute)

	if client.ChainID() != "test" {
		t.Fatalf("ChainID not forwarded, got %q", client.ChainID())
	}
}

func TestWithCachedValset(t *testing.T) {
	inner := newMockClient(0)
	fn := state.WithCachedValset(func() (state.Client, error) {
		return inner, nil
	}, time.Minute)

	client, err := fn()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	_, _ = client.Head(ctx)
	_, _ = client.Head(ctx)
	_, _ = client.GetByHeight(ctx, 10)

	if calls := inner.headCalls.Load(); calls != 1 {
		t.Fatalf("expected 1 inner call through WithCachedValset, got %d", calls)
	}
}
