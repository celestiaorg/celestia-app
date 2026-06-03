package fibre

import (
	"context"

	fibregrpc "github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	core "github.com/cometbft/cometbft/types"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// withHostRefresh runs do against the validator's cached gRPC client. If it
// fails in a way a changed host could explain, it re-queries state once
// (rate-limited to once per block time per validator) to check whether the
// validator's host changed. When the host changed to a valid new value, it
// evicts the stale client, re-dials, and retries do exactly once. Otherwise it
// returns the original error.
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

	// err carries whichever stage failed: the dial (client == nil) or, once a
	// client is in hand, the RPC.
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
	// A successful dial followed by an application-level error (NotFound,
	// InvalidArgument, ...) is not a host problem, so don't spend a state query
	// on it. Only a failed dial (client == nil, e.g. an invalid host) or a
	// transport error (an unreachable or timed-out peer) can be explained by a
	// changed host.
	if client != nil && !isUnreachable(err) {
		return zero, err
	}

	changed, valid, _ := c.state.RefreshHost(ctx, val)
	if !changed {
		return zero, err
	}
	// The host changed on chain, so the cached connection points at a stale
	// address whether or not the new host is valid; drop it either way.
	c.clientCache.Evict(val)
	if !valid {
		return zero, err
	}

	client, err = c.clientCache.GetClient(ctx, val)
	if err != nil {
		return zero, err
	}
	return do(client)
}

// isUnreachable reports whether err is a transport-level gRPC error that a
// changed host could explain: the peer was unreachable or timed out, as opposed
// to an application error returned by a reachable server.
func isUnreachable(err error) bool {
	switch status.Code(err) {
	case grpccodes.Unavailable, grpccodes.DeadlineExceeded:
		return true
	default:
		return false
	}
}
