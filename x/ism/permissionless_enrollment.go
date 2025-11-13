package ism

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"cosmossdk.io/errors"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/keeper"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
)

// PermissionlessISMEnrollment handles pure permissionless ISM route enrollment for module-owned RoutingISMs
type PermissionlessISMEnrollment struct {
	keeper     *ismkeeper.Keeper
	moduleAddr string
}

// NewPermissionlessISMEnrollment creates a new permissionless ISM enrollment handler
func NewPermissionlessISMEnrollment(keeper *ismkeeper.Keeper, moduleName string) *PermissionlessISMEnrollment {
	return &PermissionlessISMEnrollment{
		keeper:     keeper,
		moduleAddr: authtypes.NewModuleAddress(moduleName).String(),
	}
}

// SetRoutingIsmDomainPermissionless sets an ISM route without requiring ownership.
// This is the PURE PERMISSIONLESS version - if the RoutingISM is module-owned, anyone can set routes.
func (p *PermissionlessISMEnrollment) SetRoutingIsmDomainPermissionless(
	ctx context.Context,
	req *ismtypes.MsgSetRoutingIsmDomain,
) (*ismtypes.MsgSetRoutingIsmDomainResponse, error) {
	// Get routing ISM
	routingISM, err := p.getRoutingIsm(ctx, req.IsmId)
	if err != nil {
		return nil, err
	}

	// Check ownership model
	if routingISM.Owner == p.moduleAddr {
		// Module-owned: PURE PERMISSIONLESS - just enroll!
		// No checks, no verification, no approval
		return p.setRoute(ctx, req, routingISM)
	} else if routingISM.Owner != "" {
		// User-owned: require ownership
		if routingISM.Owner != req.Owner {
			return nil, errors.Wrap(ismtypes.ErrInvalidOwner, fmt.Sprintf("%s does not own RoutingISM %s", req.Owner, req.IsmId.String()))
		}
		return p.setRoute(ctx, req, routingISM)
	} else {
		// Ownerless (renounced): anyone can enroll
		return p.setRoute(ctx, req, routingISM)
	}
}

// setRoute performs the actual route enrollment
func (p *PermissionlessISMEnrollment) setRoute(
	ctx context.Context,
	req *ismtypes.MsgSetRoutingIsmDomain,
	routingISM *ismtypes.RoutingISM,
) (*ismtypes.MsgSetRoutingIsmDomainResponse, error) {
	// Check if the ISM we want to route to exists
	exists, err := p.keeper.GetCoreKeeper().IsmExists(ctx, req.Route.Ism)
	if err != nil || !exists {
		return nil, errors.Wrapf(ismtypes.ErrUnkownIsmId, "ISM %s not found", req.Route.Ism.String())
	}

	// Check if route already exists (first-enrollment-wins)
	for _, route := range routingISM.Routes {
		if route.Domain == req.Route.Domain {
			return nil, fmt.Errorf("route already enrolled for domain %d (first-enrollment-wins)", req.Route.Domain)
		}
	}

	// Set the domain route
	routingISM.SetDomain(req.Route)

	// Write to KV store
	if err = p.keeper.GetIsms().Set(ctx, routingISM.Id.GetInternalId(), routingISM); err != nil {
		return nil, errors.Wrap(ismtypes.ErrUnexpectedError, err.Error())
	}

	// Emit event
	_ = sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&ismtypes.EventSetRoutingIsmDomain{
		Owner:       req.Owner,
		IsmId:       req.IsmId,
		RouteIsmId:  req.Route.Ism,
		RouteDomain: req.Route.Domain,
	})

	return &ismtypes.MsgSetRoutingIsmDomainResponse{}, nil
}

// getRoutingIsm retrieves a RoutingISM by ID
func (p *PermissionlessISMEnrollment) getRoutingIsm(ctx context.Context, ismId util.HexAddress) (*ismtypes.RoutingISM, error) {
	ism, err := p.keeper.GetIsms().Get(ctx, ismId.GetInternalId())
	if err != nil {
		return nil, errors.Wrap(ismtypes.ErrUnkownIsmId, fmt.Sprintf("RoutingISM %s not found", ismId.String()))
	}

	routingISM, ok := ism.(*ismtypes.RoutingISM)
	if !ok {
		return nil, errors.Wrap(ismtypes.ErrInvalidIsmType, "ISM is not a RoutingISM")
	}

	return routingISM, nil
}

// TransferRoutingIsmOwnership transfers RoutingISM ownership to the module account for pure permissionless enrollment
func (p *PermissionlessISMEnrollment) TransferRoutingIsmOwnership(
	ctx context.Context,
	ismId util.HexAddress,
	currentOwner string,
) error {
	routingISM, err := p.getRoutingIsm(ctx, ismId)
	if err != nil {
		return err
	}

	if routingISM.Owner != currentOwner {
		return errors.Wrap(ismtypes.ErrInvalidOwner, fmt.Sprintf("%s does not own RoutingISM %s", currentOwner, ismId.String()))
	}

	// Transfer ownership to module
	routingISM.Owner = p.moduleAddr

	if err = p.keeper.GetIsms().Set(ctx, routingISM.Id.GetInternalId(), routingISM); err != nil {
		return errors.Wrap(ismtypes.ErrUnexpectedError, err.Error())
	}

	_ = sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&ismtypes.EventSetRoutingIsm{
		Owner:             currentOwner,
		IsmId:             ismId,
		NewOwner:          p.moduleAddr,
		RenounceOwnership: false,
	})

	return nil
}
