package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the blob MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

// RegisterEVMAddress verifies that the validator exists on chain. It then stores the EVM address.
// If it already exists, it will simply overwrite the previous value
func (k Keeper) RegisterEVMAddress(goCtx context.Context, msg *types.MsgRegisterEVMAddress) (*types.MsgRegisterEVMAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	valAddr, err := sdk.ValAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		return nil, err
	}

	if _, exists := k.StakingKeeper.GetValidator(ctx, valAddr); !exists {
		return nil, staking.ErrNoValidatorFound
	}

	k.SetEVMAddress(ctx, msg.ValidatorAddress, msg.EvmAddress)

	return &types.MsgRegisterEVMAddressResponse{}, nil
}
