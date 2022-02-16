package keeper

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
)

// ValsetConfirm queries the ValsetConfirm of the qgb module
func (k Keeper) ValsetConfirm(
	c context.Context,
	req *types.QueryValsetConfirmRequest) (*types.QueryValsetConfirmResponse, error) {
	// TODO
	return &types.QueryValsetConfirmResponse{}, nil
}
