package state

import (
	"context"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	core "github.com/cometbft/cometbft/types"
)

// CachingClient wraps a [Client] and caches the validator set returned by the
// inner client's [validator.SetGetter]. Cached data is refreshed when the TTL
// expires. All other [Client] methods are forwarded directly.
//
// [Head] returns the cached validators paired with the height from the most
// recent fetch. When the cache is stale it re-fetches before returning.
//
// [GetByHeight] returns the cached validators paired with the caller-supplied
// height. It never calls through to the inner client once the cache is seeded.
//
// A TTL of 0 disables automatic refresh — the validator set is fetched exactly
// once. This is appropriate for networks with a constant validator set.
type CachingClient struct {
	Client
	ttl time.Duration

	mu        sync.Mutex
	valset    *core.ValidatorSet
	height    uint64
	fetchedAt time.Time
}

// NewCachingClient returns a [CachingClient] that delegates to inner for all
// operations, but caches the validator set for the given TTL.
func NewCachingClient(inner Client, ttl time.Duration) *CachingClient {
	return &CachingClient{Client: inner, ttl: ttl}
}

// WithCachedValset wraps a [Client] constructor so the returned client caches
// the validator set with the given TTL. See [CachingClient] for details.
func WithCachedValset(fn func() (Client, error), ttl time.Duration) func() (Client, error) {
	return func() (Client, error) {
		inner, err := fn()
		if err != nil {
			return nil, err
		}
		return NewCachingClient(inner, ttl), nil
	}
}

// Head returns the cached validator set and height, re-fetching from the inner
// [Client] when the cache has not been seeded or the TTL has elapsed.
func (c *CachingClient) Head(ctx context.Context) (validator.Set, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.refreshLocked(ctx); err != nil {
		return validator.Set{}, err
	}
	return validator.Set{ValidatorSet: c.valset, Height: c.height}, nil
}

// GetByHeight returns the cached validators paired with the requested height.
// If the cache has not been seeded yet, it fetches once via [Head] on the inner
// client.
func (c *CachingClient) GetByHeight(ctx context.Context, height uint64) (validator.Set, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.valset == nil {
		if err := c.refreshLocked(ctx); err != nil {
			return validator.Set{}, err
		}
	}
	return validator.Set{ValidatorSet: c.valset, Height: height}, nil
}

// refreshLocked fetches the validator set from the inner client and updates the
// cache. Must be called with c.mu held.
func (c *CachingClient) refreshLocked(ctx context.Context) error {
	if c.valset != nil && (c.ttl == 0 || time.Since(c.fetchedAt) < c.ttl) {
		return nil
	}
	set, err := c.Client.Head(ctx)
	if err != nil {
		return err
	}
	c.valset = set.ValidatorSet
	c.height = set.Height
	c.fetchedAt = time.Now()
	return nil
}

var _ Client = (*CachingClient)(nil)
