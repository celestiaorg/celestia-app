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

// ValsetConfirm handles MsgValsetConfirm.
func (k msgServer) ValsetConfirm(
	_ context.Context,
	_ *types.MsgValsetConfirm,
) (*types.MsgValsetConfirmResponse, error) {
	// empty as per QGB ADR-005
	return &types.MsgValsetConfirmResponse{}, nil
}

// DataCommitmentConfirm handles MsgDataCommitmentConfirm.
func (k msgServer) DataCommitmentConfirm(
	_ context.Context,
	_ *types.MsgDataCommitmentConfirm,
) (*types.MsgDataCommitmentConfirmResponse, error) {
	// empty as per QGB ADR-005
	return &types.MsgDataCommitmentConfirmResponse{}, nil
}
