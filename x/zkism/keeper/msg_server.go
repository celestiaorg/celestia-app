package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
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
		Id:                  ismId,
		Owner:               msg.Creator,
		StateRoot:           msg.StateRoot,
		Height:              msg.Height,
		Namespace:           msg.Namespace,
		SequencerPublicKey:  msg.SequencerPublicKey,
		StateTransitionVkey: msg.StateTransitionVkey,
		StateMembershipVkey: msg.StateMembershipVkey,
		VkeyCommitment:      msg.VkeyCommitment,
	}

	if err := m.isms.Set(ctx, ismId.GetInternalId(), newIsm); err != nil {
		return nil, errorsmod.Wrap(err, err.Error())
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventCreateZKExecutionISM{
		Id:                  newIsm.Id,
		Owner:               newIsm.Owner,
		StateRoot:           newIsm.StateRoot,
		Height:              newIsm.Height,
		Namespace:           newIsm.Namespace,
		SequencerPublicKey:  newIsm.SequencerPublicKey,
		StateTransitionVkey: newIsm.StateTransitionVkey,
		StateMembershipVkey: newIsm.StateMembershipVkey,
		VkeyCommitment:      newIsm.VkeyCommitment,
	}); err != nil {
		return nil, err
	}

	return &types.MsgCreateZKExecutionISMResponse{
		Id: ismId,
	}, nil
}

// UpdateZKExecutionISM implements types.MsgServer.
func (m msgServer) UpdateZKExecutionISM(ctx context.Context, msg *types.MsgUpdateZKExecutionISM) (*types.MsgUpdateZKExecutionISMResponse, error) {
	ism, err := m.isms.Get(ctx, msg.Id.GetInternalId())
	if err != nil {
		return nil, err
	}

	var publicValues types.PublicValues
	if err := publicValues.Unmarshal(msg.PublicValues); err != nil {
		return nil, err
	}

	if err := m.validatePublicValues(ctx, msg.Height, ism, publicValues); err != nil {
		return nil, err
	}

	if err := types.VerifyGroth16(ctx, ism, msg.Proof, msg.PublicValues); err != nil {
		return nil, err
	}

	ism.Height = publicValues.NewHeight
	ism.StateRoot = publicValues.NewStateRoot[:]
	if err := m.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
		return nil, err
	}

	return &types.MsgUpdateZKExecutionISMResponse{}, nil
}

// UpdateParams implements types.MsgServer.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg.Authority != m.authority {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid authority; expected %s, got %s", m.authority, msg.Authority)
	}

	if err := m.params.Set(ctx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
