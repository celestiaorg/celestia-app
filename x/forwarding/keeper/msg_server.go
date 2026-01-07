package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ types.MsgServer = (*msgServer)(nil)

type msgServer struct {
	*Keeper
}

// NewMsgServerImpl returns an implementation of the forwarding MsgServer interface.
func NewMsgServerImpl(keeper *Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

// WarpForward implements types.MsgServer.
func (m *msgServer) WarpForward(ctx context.Context, msg *types.MsgWarpForward) (*types.MsgWarpForwardResponse, error) {
	forwardAddr := types.DeriveForwardAddress(msg.DerivationKeys()...)

	tokenId, err := util.DecodeHexAddress(msg.Token.Denom)
	if err != nil {
		return nil, err
	}

	msgRemoteTransfer := &warptypes.MsgRemoteTransfer{
		Sender:            forwardAddr.String(),
		TokenId:           tokenId,
		DestinationDomain: msg.DestinationDomain,
		Recipient:         msg.Recipient,
		Amount:            msg.Token.Amount,
		// TODO: remaining fields (post dispatch info, custom hook ids, gas..etc)
	}

	handler := m.msgRouter.Handler(msgRemoteTransfer)
	if handler == nil {
		return nil, types.ErrInvalidRoute
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	res, err := handler(sdkCtx, msg)
	if err != nil {
		return nil, err
	}

	// NOTE: The sdk msg handler creates a new EventManager, so events must be correctly propagated back to the current context
	sdkCtx.EventManager().EmitEvents(res.GetEvents())

	// TODO: handle response info propagation if applicable
	return &types.MsgWarpForwardResponse{}, nil
}

func (m *msgServer) CreateInterchainAccountsRouter(ctx context.Context) error {

	return nil
}

func (m *msgServer) EnrollRemoteRouter(ctx context.Context) error {

	return nil
}
