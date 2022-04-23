package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// ValsetConfirm queries the ValsetConfirm of the qgb module
func (k Keeper) ValsetConfirm(
	c context.Context,
	req *types.QueryValsetConfirmRequest) (*types.QueryValsetConfirmResponse, error) {
	addr, err := sdk.AccAddressFromBech32(req.Address)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "address invalid")
	}
	return &types.QueryValsetConfirmResponse{Confirm: k.GetValsetConfirm(sdk.UnwrapSDKContext(c), req.Nonce, addr)}, nil
}

// ValsetConfirmsByNonce queries the ValsetConfirmsByNonce of the qgb module
func (k Keeper) ValsetConfirmsByNonce(
	c context.Context,
	req *types.QueryValsetConfirmsByNonceRequest) (*types.QueryValsetConfirmsByNonceResponse, error) {
	confirms := k.GetValsetConfirms(sdk.UnwrapSDKContext(c), req.Nonce)
	return &types.QueryValsetConfirmsByNonceResponse{Confirms: confirms}, nil
}

const maxValsetRequestsReturned = 5

func (k Keeper) Params(c context.Context, request *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params := k.GetParams(sdk.UnwrapSDKContext(c))
	return &types.QueryParamsResponse{
		Params: params,
	}, nil
}
