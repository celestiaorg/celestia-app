package warp

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
)

// PermissionlessEnrollment handles pure permissionless enrollment logic for module-owned tokens
type PermissionlessEnrollment struct {
	keeper      *warpkeeper.Keeper
	moduleAddr  string
}

// NewPermissionlessEnrollment creates a new permissionless enrollment handler
func NewPermissionlessEnrollment(keeper *warpkeeper.Keeper, moduleName string) *PermissionlessEnrollment {
	return &PermissionlessEnrollment{
		keeper:     keeper,
		moduleAddr: authtypes.NewModuleAddress(moduleName).String(),
	}
}

// EnrollRemoteRouterPermissionless enrolls a warp route without requiring ownership.
// This is the PURE PERMISSIONLESS version - if the token is module-owned, anyone can enroll.
func (p *PermissionlessEnrollment) EnrollRemoteRouterPermissionless(
	ctx context.Context,
	msg *warptypes.MsgEnrollRemoteRouter,
) (*warptypes.MsgEnrollRemoteRouterResponse, error) {
	tokenId := msg.TokenId
	token, err := p.keeper.HypTokens.Get(ctx, tokenId.GetInternalId())
	if err != nil {
		return nil, fmt.Errorf("token with id %s not found", tokenId.String())
	}

	// Check ownership model
	if token.Owner == p.moduleAddr {
		// Module-owned: PURE PERMISSIONLESS - just enroll!
		// No checks, no verification, no approval
		return p.enrollRouter(ctx, msg, tokenId)
	} else if token.Owner != "" {
		// User-owned: require ownership
		if token.Owner != msg.Owner {
			return nil, fmt.Errorf("%s does not own token with id %s", msg.Owner, tokenId.String())
		}
		return p.enrollRouter(ctx, msg, tokenId)
	} else {
		// Ownerless (renounced): anyone can enroll
		return p.enrollRouter(ctx, msg, tokenId)
	}
}

// enrollRouter performs the actual enrollment
func (p *PermissionlessEnrollment) enrollRouter(
	ctx context.Context,
	msg *warptypes.MsgEnrollRemoteRouter,
	tokenId util.HexAddress,
) (*warptypes.MsgEnrollRemoteRouterResponse, error) {
	if msg.RemoteRouter == nil {
		return nil, fmt.Errorf("invalid remote router")
	}

	if msg.RemoteRouter.ReceiverContract == "" {
		return nil, fmt.Errorf("invalid receiver contract")
	}

	// Check if route already exists (first-enrollment-wins)
	exists, err := p.keeper.EnrolledRouters.Has(ctx, collections.Join(tokenId.GetInternalId(), msg.RemoteRouter.ReceiverDomain))
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("route already enrolled for domain %d (first-enrollment-wins)", msg.RemoteRouter.ReceiverDomain)
	}

	// Enroll the route
	if err = p.keeper.EnrolledRouters.Set(ctx, collections.Join(tokenId.GetInternalId(), msg.RemoteRouter.ReceiverDomain), *msg.RemoteRouter); err != nil {
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

// TransferTokenOwnership transfers token ownership to the module account for pure permissionless enrollment
func (p *PermissionlessEnrollment) TransferTokenOwnership(
	ctx context.Context,
	tokenId util.HexAddress,
	currentOwner string,
) error {
	token, err := p.keeper.HypTokens.Get(ctx, tokenId.GetInternalId())
	if err != nil {
		return fmt.Errorf("token with id %s not found", tokenId.String())
	}

	if token.Owner != currentOwner {
		return fmt.Errorf("%s does not own token with id %s", currentOwner, tokenId.String())
	}

	// Transfer ownership to module
	token.Owner = p.moduleAddr

	err = p.keeper.HypTokens.Set(ctx, tokenId.GetInternalId(), token)
	if err != nil {
		return err
	}

	_ = sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&warptypes.EventSetToken{
		TokenId:           tokenId.String(),
		Owner:             currentOwner,
		NewOwner:          p.moduleAddr,
		RenounceOwnership: false,
	})

	return nil
}
