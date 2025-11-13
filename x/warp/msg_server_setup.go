package warp

import (
	"context"
	"fmt"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	corekeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/keeper"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	ismkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/keeper"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	hookkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/keeper"
	hooktypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	celestiawarptypes "github.com/celestiaorg/celestia-app/v6/x/warp/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// SetupMsgServer handles setup of permissionless infrastructure
type SetupMsgServer struct {
	warpKeeper      *warpkeeper.Keeper
	hyperlaneKeeper *corekeeper.Keeper
	moduleAddr      string
}

// NewSetupMsgServer creates a new setup message server
func NewSetupMsgServer(warpKeeper *warpkeeper.Keeper, hyperlaneKeeper *corekeeper.Keeper) celestiawarptypes.MsgServer {
	return &SetupMsgServer{
		warpKeeper:      warpKeeper,
		hyperlaneKeeper: hyperlaneKeeper,
		moduleAddr:      authtypes.NewModuleAddress("warp").String(),
	}
}

// SetupPermissionlessInfrastructure creates or transfers infrastructure to module ownership
func (ms *SetupMsgServer) SetupPermissionlessInfrastructure(
	ctx context.Context,
	msg *celestiawarptypes.MsgSetupPermissionlessInfrastructure,
) (*celestiawarptypes.MsgSetupPermissionlessInfrastructureResponse, error) {
	switch msg.Mode {
	case "create":
		return ms.createInfrastructure(ctx, msg)
	case "transfer":
		return ms.transferInfrastructure(ctx, msg)
	default:
		return nil, fmt.Errorf("invalid mode: %s (must be 'create' or 'transfer')", msg.Mode)
	}
}

// createInfrastructure creates new module-owned mailbox, ISM, and token
// This uses internal keeper methods to create resources owned by the module
func (ms *SetupMsgServer) createInfrastructure(
	ctx context.Context,
	msg *celestiawarptypes.MsgSetupPermissionlessInfrastructure,
) (*celestiawarptypes.MsgSetupPermissionlessInfrastructureResponse, error) {
	if msg.LocalDomain == 0 {
		return nil, fmt.Errorf("local_domain is required for create mode")
	}
	if msg.OriginDenom == "" {
		return nil, fmt.Errorf("origin_denom is required for create mode")
	}

	// Step 1: Create Routing ISM using ISM keeper, then transfer ownership
	// We have to create as the creator first, then modify the owner
	ismMsgServer := ismkeeper.NewMsgServerImpl(&ms.hyperlaneKeeper.IsmKeeper)
	routingIsmRes, err := ismMsgServer.CreateRoutingIsm(ctx, &ismtypes.MsgCreateRoutingIsm{
		Creator: msg.Creator,
		Routes:  []ismtypes.Route{}, // Empty routes initially
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create routing ISM: %w", err)
	}
	routingIsmID := routingIsmRes.Id

	// Transfer ISM ownership to module
	if err := ms.transferRoutingISMOwnership(ctx, routingIsmID, msg.Creator); err != nil {
		return nil, fmt.Errorf("failed to transfer ISM ownership: %w", err)
	}

	// Step 2: Create Mailbox using core keeper (without hooks initially)
	coreMsgServer := corekeeper.NewMsgServerImpl(ms.hyperlaneKeeper)
	mailboxRes, err := coreMsgServer.CreateMailbox(ctx, &coretypes.MsgCreateMailbox{
		Owner:        msg.Creator,
		LocalDomain:  msg.LocalDomain,
		DefaultIsm:   routingIsmID,
		DefaultHook:  nil,
		RequiredHook: nil,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create mailbox: %w", err)
	}
	mailboxID := mailboxRes.Id

	// Step 2.1: Create MerkleTreeHook for the mailbox
	hookMsgServer := hookkeeper.NewMsgServerImpl(&ms.hyperlaneKeeper.PostDispatchKeeper)
	merkleHookRes, err := hookMsgServer.CreateMerkleTreeHook(ctx, &hooktypes.MsgCreateMerkleTreeHook{
		Owner:     msg.Creator,
		MailboxId: mailboxID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create merkle tree hook: %w", err)
	}
	hookID := merkleHookRes.Id

	// Step 2.2: Set the merkle tree hook as required hook on mailbox
	_, err = coreMsgServer.SetMailbox(ctx, &coretypes.MsgSetMailbox{
		Owner:        msg.Creator,
		MailboxId:    mailboxID,
		DefaultHook:  &hookID,
		RequiredHook: &hookID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to set hooks on mailbox: %w", err)
	}

	// Step 2.3: Transfer mailbox ownership to module
	if err := ms.transferMailboxOwnership(ctx, mailboxID, msg.Creator); err != nil {
		return nil, fmt.Errorf("failed to transfer mailbox ownership: %w", err)
	}

	// Step 3: Create Collateral Token using warp keeper, then transfer ownership
	warpMsgServer := warpkeeper.NewMsgServerImpl(*ms.warpKeeper)
	tokenRes, err := warpMsgServer.CreateCollateralToken(ctx, &warptypes.MsgCreateCollateralToken{
		Owner:         msg.Creator,
		OriginMailbox: mailboxID,
		OriginDenom:   msg.OriginDenom,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create collateral token: %w", err)
	}
	tokenID := tokenRes.Id

	// Transfer token ownership to module
	if err := ms.transferTokenOwnership(ctx, tokenID, msg.Creator); err != nil {
		return nil, fmt.Errorf("failed to transfer token ownership: %w", err)
	}

	// TODO: Emit event once proto events are properly generated
	// sdkCtx := sdk.UnwrapSDKContext(ctx)
	// _ = sdkCtx.EventManager().EmitTypedEvent(&celestiawarptypes.EventSetupPermissionlessInfrastructure{...})

	return &celestiawarptypes.MsgSetupPermissionlessInfrastructureResponse{
		MailboxId:    mailboxID,
		RoutingIsmId: routingIsmID,
		TokenId:      tokenID,
	}, nil
}

// transferInfrastructure transfers existing resources to module ownership
func (ms *SetupMsgServer) transferInfrastructure(
	ctx context.Context,
	msg *celestiawarptypes.MsgSetupPermissionlessInfrastructure,
) (*celestiawarptypes.MsgSetupPermissionlessInfrastructureResponse, error) {
	if msg.CurrentOwner == "" {
		return nil, fmt.Errorf("current_owner is required for transfer mode")
	}

	var mailboxID, routingIsmID, tokenID util.HexAddress

	// Transfer Routing ISM if provided
	if msg.ExistingRoutingIsmId != nil {
		routingIsmID = *msg.ExistingRoutingIsmId
		if err := ms.transferRoutingISMOwnership(ctx, routingIsmID, msg.CurrentOwner); err != nil {
			return nil, fmt.Errorf("failed to transfer routing ISM: %w", err)
		}
	}

	// Transfer Mailbox if provided
	if msg.ExistingMailboxId != nil {
		mailboxID = *msg.ExistingMailboxId
		if err := ms.transferMailboxOwnership(ctx, mailboxID, msg.CurrentOwner); err != nil {
			return nil, fmt.Errorf("failed to transfer mailbox: %w", err)
		}
	}

	// Transfer Token if provided
	if msg.ExistingTokenId != nil {
		tokenID = *msg.ExistingTokenId
		if err := ms.transferTokenOwnership(ctx, tokenID, msg.CurrentOwner); err != nil {
			return nil, fmt.Errorf("failed to transfer token: %w", err)
		}
	}

	// TODO: Emit event once proto events are properly generated
	// sdkCtx := sdk.UnwrapSDKContext(ctx)
	// _ = sdkCtx.EventManager().EmitTypedEvent(&celestiawarptypes.EventSetupPermissionlessInfrastructure{...})

	return &celestiawarptypes.MsgSetupPermissionlessInfrastructureResponse{
		MailboxId:    mailboxID,
		RoutingIsmId: routingIsmID,
		TokenId:      tokenID,
	}, nil
}

// transferRoutingISMOwnership transfers a routing ISM to module ownership
func (ms *SetupMsgServer) transferRoutingISMOwnership(
	ctx context.Context,
	ismID util.HexAddress,
	currentOwner string,
) error {
	//  Get ISM using UpdateRoutingIsmOwner message instead
	err := ms.hyperlaneKeeper.IsmKeeper.UpdateRoutingIsmOwner(ctx, &ismtypes.MsgUpdateRoutingIsmOwner{
		IsmId:             ismID,
		Owner:             currentOwner,
		NewOwner:          ms.moduleAddr,
		RenounceOwnership: false,
	})
	if err != nil {
		return fmt.Errorf("failed to transfer ISM ownership: %w", err)
	}

	return nil
}

// transferMailboxOwnership transfers a mailbox to module ownership
func (ms *SetupMsgServer) transferMailboxOwnership(
	ctx context.Context,
	mailboxID util.HexAddress,
	currentOwner string,
) error {
	// Use SetMailbox message to transfer ownership
	coreMsgServer := corekeeper.NewMsgServerImpl(ms.hyperlaneKeeper)
	_, err := coreMsgServer.SetMailbox(ctx, &coretypes.MsgSetMailbox{
		Owner:             currentOwner,
		MailboxId:         mailboxID,
		NewOwner:          ms.moduleAddr,
		RenounceOwnership: false,
	})
	if err != nil {
		return fmt.Errorf("failed to transfer mailbox ownership: %w", err)
	}

	return nil
}

// transferTokenOwnership transfers a token to module ownership
func (ms *SetupMsgServer) transferTokenOwnership(
	ctx context.Context,
	tokenID util.HexAddress,
	currentOwner string,
) error {
	// Use SetToken message to transfer ownership
	warpMsgServer := warpkeeper.NewMsgServerImpl(*ms.warpKeeper)
	_, err := warpMsgServer.SetToken(ctx, &warptypes.MsgSetToken{
		Owner:    currentOwner,
		TokenId:  tokenID,
		NewOwner: ms.moduleAddr,
		IsmId:    nil, // Not changing ISM
	})
	if err != nil {
		return fmt.Errorf("failed to transfer token ownership: %w", err)
	}

	return nil
}
