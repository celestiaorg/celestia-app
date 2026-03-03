package keeper

import (
	"context"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v6/x/valaddr/types"
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

	consPubKey, err := validator.ConsPubKey()
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidValidator, "failed to get consensus public key: %v", err)
	}
	consAddr := sdk.ConsAddress(consPubKey.Address())

	// Validate address length (supports IP addresses, DNS names, etc.)
	if len(msg.Host) > types.MaxHostLen {
		return nil, errors.Wrapf(types.ErrInvalidHostAddress, "address must be less or equal than %d characters, got %d", types.MaxHostLen, len(msg.Host))
	}
	if len(msg.Host) == 0 {
		return nil, errors.Wrap(types.ErrInvalidHostAddress, "address cannot be empty")
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
