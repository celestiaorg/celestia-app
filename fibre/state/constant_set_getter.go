package state

import (
	"context"
	"sync"

	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
)

// ConstantValsetClient wraps a [Client] and caches the validator set returned by
// the first successful call to the underlying client's [validator.SetGetter].
// All subsequent calls to [Head] and [GetByHeight] return the cached set,
// ignoring the requested height. All other [Client] methods are forwarded
// directly to the inner client.
//
// This wrapper MUST only be used for networks whose validator set is known to be
// constant (e.g. a single-validator devnet or a frozen testnet). Using it on a
// network where validators can change will silently return stale data and break
// shard assignment, signature verification, and download scheduling.
type ConstantValsetClient struct {
	Client

	once sync.Once
	set  validator.Set
	err  error
}

// NewConstantValsetClient returns a [ConstantValsetClient] that delegates to
// inner for all operations, but caches the first validator set and returns it
// for every subsequent [Head] and [GetByHeight] call.
func NewConstantValsetClient(inner Client) *ConstantValsetClient {
	return &ConstantValsetClient{Client: inner}
}

// WithConstantValset wraps a [Client] constructor function so that the returned
// client caches the validator set after the first fetch. See
// [ConstantValsetClient] for caveats.
func WithConstantValset(fn func() (Client, error)) func() (Client, error) {
	return func() (Client, error) {
		inner, err := fn()
		if err != nil {
			return nil, err
		}
		return NewConstantValsetClient(inner), nil
	}
}

// Head returns the cached validator set, fetching it from the underlying
// [Client] on the first call.
func (c *ConstantValsetClient) Head(ctx context.Context) (validator.Set, error) {
	c.once.Do(func() {
		c.set, c.err = c.Client.Head(ctx)
	})
	return c.set, c.err
}

// GetByHeight returns the cached validator set, ignoring the height parameter.
// The set is fetched from the underlying [Client] on the first call to either
// [Head] or [GetByHeight].
func (c *ConstantValsetClient) GetByHeight(ctx context.Context, _ uint64) (validator.Set, error) {
	return c.Head(ctx)
}

var _ Client = (*ConstantValsetClient)(nil)
