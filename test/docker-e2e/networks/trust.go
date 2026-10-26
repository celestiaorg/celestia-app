package networks

import (
	"context"
	"errors"
	"fmt"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
)

// FetchTrustBlock tries each configured RPC so one unavailable provider does not
// prevent state-sync bootstrap. Every response must belong to the expected chain.
func FetchTrustBlock(ctx context.Context, cfg *Config, offset int64) (*rpctypes.ResultBlock, error) {
	if len(cfg.RPCs) == 0 {
		return nil, errors.New("no RPC endpoints configured")
	}
	var failures []error
	for i, endpoint := range cfg.RPCs {
		block, err := fetchTrustBlock(ctx, endpoint, cfg.ChainID, offset)
		if err == nil {
			return block, nil
		}
		failures = append(failures, fmt.Errorf("RPC %d: %w", i+1, err))
	}
	return nil, fmt.Errorf("no RPC supplied a usable trust block: %w", errors.Join(failures...))
}

func fetchTrustBlock(ctx context.Context, endpoint, chainID string, offset int64) (*rpctypes.ResultBlock, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client, err := rpchttp.New(endpoint, "/websocket")
	if err != nil {
		return nil, err
	}
	status, err := client.Status(ctx)
	if err != nil {
		return nil, err
	}
	if status == nil || status.NodeInfo.Network != chainID || status.SyncInfo.CatchingUp {
		return nil, errors.New("RPC is not synced to the expected chain")
	}
	if offset <= 0 || status.SyncInfo.LatestBlockHeight <= offset {
		return nil, errors.New("invalid trust height offset")
	}
	height := status.SyncInfo.LatestBlockHeight - offset
	block, err := client.Block(ctx, &height)
	if err != nil {
		return nil, err
	}
	if block == nil || block.Block == nil || block.Block.ChainID != chainID || block.Block.Height != height || len(block.BlockID.Hash) == 0 {
		return nil, errors.New("RPC returned an invalid trust block")
	}
	return block, nil
}
