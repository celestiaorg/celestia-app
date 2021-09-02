package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/payment/types"
)

var _ types.MsgServer = msgServer{}

// MsgServer is the server API for Msg service.
type MsgServer interface {
	// PayForMessage allows the user to post data to made be available.
	PayForMessage(context.Context, *types.MsgWirePayForMessage) (*types.MsgPayForMessageResponse, error)
	// PayForMessage allows the user to post data to made be available.
	SignedTransactionDataPayForMessage(context.Context, *types.SignedTransactionDataPayForMessage) (*types.SignedTransactionDataPayForMessageResponse, error)
}

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the bank MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) MsgServer {
	return &msgServer{Keeper: keeper}
}
