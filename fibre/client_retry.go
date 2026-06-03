package fibre

import (
	"context"

	fibregrpc "github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	core "github.com/cometbft/cometbft/types"
)

// withHostRefresh runs do against the validator's cached gRPC client. If it
// fails, it re-queries state once (rate-limited to once per block time per
// validator) to check whether the validator's host changed. When the host
// changed to a valid new value, it evicts the stale client, re-dials, and
// retries do exactly once. Otherwise it returns the original error.
//
// This recovers from a validator that registered an invalid/unreachable host
// and later corrected it, without requiring a client restart.
func withHostRefresh[R any](
	c *Client,
	ctx context.Context,
	val *core.Validator,
	do func(fibregrpc.Client) (R, error),
) (R, error) {
	var zero R

	client, err := c.clientCache.GetClient(ctx, val)
	if err == nil {
		var resp R
		if resp, err = do(client); err == nil {
			return resp, nil
		}
	}
	// Don't refresh on a cancelled context: the failure is the caller leaving,
	// not a stale host.
	if ctx.Err() != nil {
		return zero, err
	}

	changed, valid, _ := c.state.RefreshHost(ctx, val)
	if !changed || !valid {
		return zero, err
	}

	c.clientCache.Evict(val)
	client, retryErr := c.clientCache.GetClient(ctx, val)
	if retryErr != nil {
		return zero, retryErr
	}
	return do(client)
}
