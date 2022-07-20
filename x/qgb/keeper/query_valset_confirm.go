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
	req *types.QueryValsetConfirmRequest,
) (*types.QueryValsetConfirmResponse, error) {
	addr, err := sdk.AccAddressFromBech32(req.Address)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "address invalid")
	}
	// _ because if the attestation is not found, the method will return nil
	// and we want to bubble the nil to the user.
	confirm, _, err := k.GetValsetConfirm(sdk.UnwrapSDKContext(c), req.Nonce, addr)
	if err != nil {
		return nil, err
	}
	return &types.QueryValsetConfirmResponse{Confirm: confirm}, nil
}

// ValsetConfirmsByNonce queries the ValsetConfirmsByNonce of the qgb module
func (k Keeper) ValsetConfirmsByNonce(
	c context.Context,
	req *types.QueryValsetConfirmsByNonceRequest,
) (*types.QueryValsetConfirmsByNonceResponse, error) {
	confirms := k.GetValsetConfirms(sdk.UnwrapSDKContext(c), req.Nonce)
	return &types.QueryValsetConfirmsByNonceResponse{Confirms: confirms}, nil
}
