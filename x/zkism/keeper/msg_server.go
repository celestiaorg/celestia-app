package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/x/zkism/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
}

// NewMsgServerImpl creates and returns a new module MsgServer instance.
func NewMsgServerImpl(keeper *Keeper) types.MsgServer {
	return &msgServer{keeper}
}

// CreateZKExecutionISM implements types.MsgServer.
func (m msgServer) CreateZKExecutionISM(ctx context.Context, msg *types.MsgCreateZKExecutionISM) (*types.MsgCreateZKExecutionISMResponse, error) {
	ismId, err := m.coreKeeper.IsmRouter().GetNextSequence(ctx, types.InterchainSecurityModuleTypeZKExecution)
	if err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	newIsm := types.ZKExecutionISM{
		Id:                         ismId,
		Owner:                      msg.Creator,
		StateRoot:                  msg.StateRoot,
		Height:                     msg.Height,
		StateTransitionVerifierKey: msg.StateTransitionVerifierKey,
		StateMembershipVerifierKey: msg.StateMembershipVerifierKey,
	}

	// validate

	if err := m.isms.Set(ctx, ismId.GetInternalId(), newIsm); err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventCreateZKExecutionISM{
		Id:                         newIsm.Id,
		Owner:                      newIsm.Owner,
		StateRoot:                  newIsm.StateRoot,
		Height:                     newIsm.Height,
		StateTransitionVerifierKey: newIsm.StateTransitionVerifierKey,
		StateMembershipVerifierKey: newIsm.StateMembershipVerifierKey,
	}); err != nil {
		return nil, err
	}

	return &types.MsgCreateZKExecutionISMResponse{
		Id: ismId,
	}, nil
}
