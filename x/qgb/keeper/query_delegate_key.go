package keeper

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
)

func (k Keeper) GetDelegateKeyByOrchestrator(
	c context.Context,
	req *types.QueryDelegateKeysByOrchestratorAddress) (*types.QueryGetDelegateKeyByOrchestratorResponse, error) {
	// TODO
	return &types.QueryGetDelegateKeyByOrchestratorResponse{}, nil
}
