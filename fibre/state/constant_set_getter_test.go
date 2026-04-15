package state_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/fibre/state"
	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	core "github.com/cometbft/cometbft/types"
)

// countingClient tracks calls to the inner SetGetter methods.
type countingClient struct {
	headCalls atomic.Int64
}

func (c *countingClient) Head(context.Context) (validator.Set, error) {
	c.headCalls.Add(1)
	return validator.Set{ValidatorSet: &core.ValidatorSet{}, Height: 42}, nil
}

func (c *countingClient) GetByHeight(_ context.Context, h uint64) (validator.Set, error) {
	c.headCalls.Add(1)
	return validator.Set{ValidatorSet: &core.ValidatorSet{}, Height: h}, nil
}

func (c *countingClient) GetHost(_ context.Context, _ *core.Validator) (validator.Host, error) {
	return "", nil
}

func (c *countingClient) ChainID() string { return "test" }

func (c *countingClient) VerifyPromise(context.Context, *state.PaymentPromise) (state.VerifiedPromise, error) {
	return state.VerifiedPromise{}, nil
}

func (c *countingClient) Start(context.Context) error { return nil }
func (c *countingClient) Stop(context.Context) error  { return nil }

func TestConstantValsetClient_CachesSet(t *testing.T) {
	inner := &countingClient{}
	var client state.Client = state.NewConstantValsetClient(inner)

	ctx := context.Background()

	// Multiple Head calls should only trigger one inner call.
	for range 5 {
		set, err := client.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if set.Height != 42 {
			t.Fatalf("expected height 42, got %d", set.Height)
		}
	}

	// GetByHeight calls should also return the cached set.
	for _, h := range []uint64{1, 100, 999} {
		set, err := client.GetByHeight(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if set.Height != 42 {
			t.Fatalf("GetByHeight(%d): expected cached height 42, got %d", h, set.Height)
		}
	}

	if calls := inner.headCalls.Load(); calls != 1 {
		t.Fatalf("expected 1 inner call, got %d", calls)
	}
}

func TestConstantValsetClient_DelegatesOtherMethods(t *testing.T) {
	inner := &countingClient{}
	var client state.Client = state.NewConstantValsetClient(inner)

	if client.ChainID() != "test" {
		t.Fatalf("ChainID not forwarded, got %q", client.ChainID())
	}
}

func TestWithConstantValset(t *testing.T) {
	inner := &countingClient{}
	fn := state.WithConstantValset(func() (state.Client, error) {
		return inner, nil
	})

	client, err := fn()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	_, _ = client.Head(ctx)
	_, _ = client.Head(ctx)
	_, _ = client.GetByHeight(ctx, 10)

	if calls := inner.headCalls.Load(); calls != 1 {
		t.Fatalf("expected 1 inner call through WithConstantValset, got %d", calls)
	}
}
