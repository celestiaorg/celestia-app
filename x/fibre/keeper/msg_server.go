package keeper

import (
	"context"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the fibre MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

// SetFibreProviderInfo implements the MsgSetFibreProviderInfo message handler
func (ms msgServer) SetFibreProviderInfo(goCtx context.Context, msg *types.MsgSetFibreProviderInfo) (*types.MsgSetFibreProviderInfoResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	
	// Parse validator address
	validatorAddr, err := sdk.ValAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidValidatorAddress, "invalid validator address: %v", err)
	}

	// Verify that the signer is the validator
	signers := msg.GetSigners()
	if len(signers) == 0 {
		return nil, types.ErrUnauthorized
	}
	signerAddr := sdk.AccAddress(validatorAddr)
	if !signers[0].Equals(signerAddr) {
		return nil, types.ErrUnauthorized
	}

	// Check if validator is in active set
	isActive, err := ms.Keeper.IsValidatorActive(ctx, validatorAddr)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidValidatorAddress, "error checking validator status: %v", err)
	}
	if !isActive {
		return nil, types.ErrValidatorNotActive
	}

	// Set the fibre provider info
	info := types.FibreProviderInfo{
		IpAddress: msg.IpAddress,
	}
	ms.Keeper.SetFibreProviderInfo(ctx, validatorAddr, info)

	// Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSetFibreProviderInfo,
			sdk.NewAttribute(types.AttributeValidatorAddress, msg.ValidatorAddress),
			sdk.NewAttribute(types.AttributeIPAddress, msg.IpAddress),
		),
	)

	return &types.MsgSetFibreProviderInfoResponse{}, nil
}

// RemoveFibreProviderInfo implements the MsgRemoveFibreProviderInfo message handler
func (ms msgServer) RemoveFibreProviderInfo(goCtx context.Context, msg *types.MsgRemoveFibreProviderInfo) (*types.MsgRemoveFibreProviderInfoResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	
	// Parse validator address
	validatorAddr, err := sdk.ValAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidValidatorAddress, "invalid validator address: %v", err)
	}

	// Check if provider info exists
	if !ms.Keeper.HasFibreProviderInfo(ctx, validatorAddr) {
		return nil, types.ErrProviderInfoNotFound
	}

	// Check if validator is still active (if so, cannot remove)
	isActive, err := ms.Keeper.IsValidatorActive(ctx, validatorAddr)
	if err != nil {
		return nil, errors.Wrapf(types.ErrInvalidValidatorAddress, "error checking validator status: %v", err)
	}
	if isActive {
		return nil, types.ErrValidatorStillActive
	}

	// Remove the fibre provider info
	ms.Keeper.RemoveFibreProviderInfo(ctx, validatorAddr)

	// Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRemoveFibreProviderInfo,
			sdk.NewAttribute(types.AttributeValidatorAddress, msg.ValidatorAddress),
			sdk.NewAttribute(types.AttributeRemoverAddress, msg.RemoverAddress),
		),
	)

	return &types.MsgRemoveFibreProviderInfoResponse{}, nil
}