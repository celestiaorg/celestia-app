package keeper

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
)

// ValsetConfirm queries the ValsetConfirm of the gravity module
func (k Keeper) ValsetConfirm(
	c context.Context,
	req *types.QueryValsetConfirmRequest) (*types.QueryValsetConfirmResponse, error) {
	return &types.QueryValsetConfirmResponse{}, nil
}
