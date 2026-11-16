package networks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
)

// minStateSyncRPCs is the number of RPC servers CometBFT state sync needs: a
// primary and at least one witness.
const minStateSyncRPCs = 2

// stateSyncProbeTimeout bounds each RPC's probe. The node gives its light
// client 10 seconds to fetch and cross-check the trusted block from every
// server, so a server that cannot answer well within that fails state sync.
var stateSyncProbeTimeout = 4 * time.Second

// SelectStateSyncRPCs returns the RPCs, in their configured order, that serve
// the light blocks state sync fetches: the commit and validator set at
// trustHeight and the latest commit. The node's light client fails if any
// server stalls, so a slow or unavailable provider must not be passed to it.
func SelectStateSyncRPCs(ctx context.Context, rpcs []string, trustHeight int64) ([]string, error) {
	errs := make([]error, len(rpcs))
	var wg sync.WaitGroup
	for i, endpoint := range rpcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = probeStateSyncRPC(ctx, endpoint, trustHeight)
		}()
	}
	wg.Wait()

	var healthy []string
	var failures []error
	for i, endpoint := range rpcs {
		if errs[i] != nil {
			failures = append(failures, fmt.Errorf("%s: %w", endpoint, errs[i]))
			continue
		}
		healthy = append(healthy, endpoint)
	}
	if len(healthy) < minStateSyncRPCs {
		return nil, fmt.Errorf("only %d of %d RPCs can serve state sync, need %d: %w",
			len(healthy), len(rpcs), minStateSyncRPCs, errors.Join(failures...))
	}
	return healthy, nil
}

func probeStateSyncRPC(ctx context.Context, endpoint string, trustHeight int64) error {
	ctx, cancel := context.WithTimeout(ctx, stateSyncProbeTimeout)
	defer cancel()
	client, err := rpchttp.New(endpoint, "/websocket")
	if err != nil {
		return err
	}
	if _, err := client.Commit(ctx, &trustHeight); err != nil {
		return fmt.Errorf("commit at %d: %w", trustHeight, err)
	}
	page, perPage := 1, 100
	if _, err := client.Validators(ctx, &trustHeight, &page, &perPage); err != nil {
		return fmt.Errorf("validators at %d: %w", trustHeight, err)
	}
	if _, err := client.Commit(ctx, nil); err != nil {
		return fmt.Errorf("latest commit: %w", err)
	}
	return nil
}
