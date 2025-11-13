package warp

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
)

// PermissionlessMsgServer wraps the standard warp msg server with permissionless enrollment logic
type PermissionlessMsgServer struct {
	keeper     *warpkeeper.Keeper
	moduleAddr string
}

// NewPermissionlessMsgServer creates a new permissionless msg server wrapper
func NewPermissionlessMsgServer(keeper *warpkeeper.Keeper) warptypes.MsgServer {
	return &PermissionlessMsgServer{
		keeper:     keeper,
		moduleAddr: authtypes.NewModuleAddress("warp").String(),
	}
}

// EnrollRemoteRouter implements permissionless enrollment for module-owned tokens
func (ms *PermissionlessMsgServer) EnrollRemoteRouter(ctx context.Context, msg *warptypes.MsgEnrollRemoteRouter) (*warptypes.MsgEnrollRemoteRouterResponse, error) {
	tokenId := msg.TokenId
	token, err := ms.keeper.HypTokens.Get(ctx, tokenId.GetInternalId())
	if err != nil {
		return nil, fmt.Errorf("token with id %s not found", tokenId.String())
	}

	if msg.RemoteRouter == nil {
		return nil, fmt.Errorf("invalid remote router")
	}

	if msg.RemoteRouter.ReceiverContract == "" {
		return nil, fmt.Errorf("invalid receiver contract")
	}

	// Check ownership model
	if token.Owner == ms.moduleAddr {
		// Module-owned: PURE PERMISSIONLESS - just enroll!
		return ms.enrollRouterPermissionless(ctx, msg, tokenId)
	} else if token.Owner == "" {
		// Ownerless (renounced): anyone can enroll
		return ms.enrollRouterPermissionless(ctx, msg, tokenId)
	} else {
		// User-owned: check ownership (traditional behavior)
		if token.Owner != msg.Owner {
			return nil, fmt.Errorf("%s does not own token with id %s", msg.Owner, tokenId.String())
		}
		return ms.enrollRouterPermissionless(ctx, msg, tokenId)
	}
}

// enrollRouterPermissionless performs the actual enrollment with first-enrollment-wins protection
func (ms *PermissionlessMsgServer) enrollRouterPermissionless(
	ctx context.Context,
	msg *warptypes.MsgEnrollRemoteRouter,
	tokenId util.HexAddress,
) (*warptypes.MsgEnrollRemoteRouterResponse, error) {
	// First-enrollment-wins: Check if route already exists
	exists, err := ms.keeper.EnrolledRouters.Has(ctx, collections.Join(tokenId.GetInternalId(), msg.RemoteRouter.ReceiverDomain))
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("route already enrolled for domain %d (first-enrollment-wins)", msg.RemoteRouter.ReceiverDomain)
	}

	// Enroll the route
	if err = ms.keeper.EnrolledRouters.Set(ctx, collections.Join(tokenId.GetInternalId(), msg.RemoteRouter.ReceiverDomain), *msg.RemoteRouter); err != nil {
		return nil, err
	}

	// Emit event
	_ = sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&warptypes.EventEnrollRemoteRouter{
		TokenId:          tokenId.String(),
		Owner:            msg.Owner,
		ReceiverDomain:   msg.RemoteRouter.ReceiverDomain,
		ReceiverContract: msg.RemoteRouter.ReceiverContract,
		Gas:              msg.RemoteRouter.Gas,
	})

	return &warptypes.MsgEnrollRemoteRouterResponse{}, nil
}

// CreateCollateralToken delegates to the underlying keeper
func (ms *PermissionlessMsgServer) CreateCollateralToken(ctx context.Context, msg *warptypes.MsgCreateCollateralToken) (*warptypes.MsgCreateCollateralTokenResponse, error) {
	standardMsgServer := warpkeeper.NewMsgServerImpl(*ms.keeper)
	return standardMsgServer.CreateCollateralToken(ctx, msg)
}

// CreateSyntheticToken delegates to the underlying keeper
func (ms *PermissionlessMsgServer) CreateSyntheticToken(ctx context.Context, msg *warptypes.MsgCreateSyntheticToken) (*warptypes.MsgCreateSyntheticTokenResponse, error) {
	standardMsgServer := warpkeeper.NewMsgServerImpl(*ms.keeper)
	return standardMsgServer.CreateSyntheticToken(ctx, msg)
}

// SetToken delegates to the underlying keeper
func (ms *PermissionlessMsgServer) SetToken(ctx context.Context, msg *warptypes.MsgSetToken) (*warptypes.MsgSetTokenResponse, error) {
	standardMsgServer := warpkeeper.NewMsgServerImpl(*ms.keeper)
	return standardMsgServer.SetToken(ctx, msg)
}

// UnrollRemoteRouter delegates to the underlying keeper
func (ms *PermissionlessMsgServer) UnrollRemoteRouter(ctx context.Context, msg *warptypes.MsgUnrollRemoteRouter) (*warptypes.MsgUnrollRemoteRouterResponse, error) {
	standardMsgServer := warpkeeper.NewMsgServerImpl(*ms.keeper)
	return standardMsgServer.UnrollRemoteRouter(ctx, msg)
}

// RemoteTransfer delegates to the underlying keeper
func (ms *PermissionlessMsgServer) RemoteTransfer(ctx context.Context, msg *warptypes.MsgRemoteTransfer) (*warptypes.MsgRemoteTransferResponse, error) {
	standardMsgServer := warpkeeper.NewMsgServerImpl(*ms.keeper)
	return standardMsgServer.RemoteTransfer(ctx, msg)
}
