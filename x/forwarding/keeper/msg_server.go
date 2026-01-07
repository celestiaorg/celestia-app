package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
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

// CreateInterchainAccountsRouter implements types.MsgServer.
func (m *msgServer) CreateInterchainAccountsRouter(ctx context.Context, msg *types.MsgCreateInterchainAccountsRouter) (*types.MsgCreateInterchainAccountsRouterResponse, error) {
	has, err := m.hypKeeper.MailboxIdExists(ctx, msg.OriginMailbox)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, fmt.Errorf("failed to find mailbox with id: %s", msg.OriginMailbox.String())
	}

	id, err := m.hypKeeper.AppRouter().GetNextSequence(ctx, uint8(types.HyperlaneModuleID))
	if err != nil {
		return nil, err
	}

	router := types.InterchainAccountsRouter{
		Id: id,
		// IsmId: ,
		OriginMailbox: msg.OriginMailbox,
		Owner:         msg.Owner,
	}

	if err = m.Routers.Set(ctx, id.GetInternalId(), router); err != nil {
		return nil, err
	}

	if err := m.RemoteRouters.Set(ctx, collections.Join(id.GetInternalId(), msg.RemoteRouter.ReceiverDomain), *msg.RemoteRouter); err != nil {
		return nil, err
	}

	return &types.MsgCreateInterchainAccountsRouterResponse{}, nil
}

// EnrollRemoteRouter implements types.MsgServer.
func (m *msgServer) EnrollRemoteRouter(ctx context.Context, msg *types.MsgEnrollRemoteRouter) (*types.MsgEnrollRemoteRouterResponse, error) {
	return &types.MsgEnrollRemoteRouterResponse{}, nil
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
