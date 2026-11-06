package keeper

import (
	"context"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v11/x/valaddr/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the valaddr MsgServer interface
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

// SetFibreProviderInfo handles the MsgSetFibreProviderInfo message
func (ms msgServer) SetFibreProviderInfo(goCtx context.Context, msg *types.MsgSetFibreProviderInfo) (*types.MsgSetFibreProviderInfoResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	valAddr, err := sdk.ValAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidValidator, "signer %v does not match a validator", msg.Signer)
	}

	validator, err := ms.stakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidValidator, "validator not found: %v", err)
	}
	if !validator.IsBonded() {
		return nil, errors.Wrap(types.ErrInvalidValidator, "validator must be bonded")
	}

	consPubKey, err := validator.ConsPubKey()
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidValidator, "failed to get consensus public key: %v", err)
	}
	consAddr := sdk.ConsAddress(consPubKey.Address())

	// Apply the full host check here too, so the stored host is valid even
	// on a path that did not run ValidateBasic.
	if err := types.ValidateHost(msg.Host); err != nil {
		return nil, err
	}

	info := types.FibreProviderInfo{
		Host: msg.Host,
	}

	if err := ms.Keeper.SetFibreProviderInfo(goCtx, consAddr, info); err != nil {
		return nil, errors.Wrap(err, "failed to set fibre provider info")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSetFibreProviderInfo,
			sdk.NewAttribute(types.AttributeKeyValidatorAddress, consAddr.String()),
			sdk.NewAttribute(types.AttributeKeyHost, msg.Host),
		),
	)

	return &types.MsgSetFibreProviderInfoResponse{}, nil
}
