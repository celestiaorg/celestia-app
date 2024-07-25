package keeper

import (
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
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
