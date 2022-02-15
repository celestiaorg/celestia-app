package keeper

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
)

func (k Keeper) drchestrator(
	c context.Context,
	req *types.QueryDelegateKeysByOrchestratorAddress) (*types.QueryDelegateKeysByOrchestratorAddressResponse, error) {
	// TODO
	return &types.QueryDelegateKeysByOrchestratorAddressResponse{}, nil
}
