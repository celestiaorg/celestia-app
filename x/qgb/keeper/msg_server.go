package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the gov MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

// ValsetConfirm handles MsgValsetConfirm
func (k msgServer) ValsetConfirm(c context.Context, msg *types.MsgValsetConfirm) (*types.MsgValsetConfirmResponse, error) {
	// TODO
	return &types.MsgValsetConfirmResponse{}, nil
}

// DataCommitmentConfirm handles MsgDataCommitmentConfirm
func (k msgServer) DataCommitmentConfirm(context.Context, *types.MsgDataCommitmentConfirm) (*types.MsgDataCommitmentConfirmResponse, error) {
	// TODO
	return &types.MsgDataCommitmentConfirmResponse{}, nil
}
