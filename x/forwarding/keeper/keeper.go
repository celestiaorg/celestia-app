package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/codec"
)

// Keeper maintains state for the forwarding module.
// The module currently has no state; this exists to satisfy future wiring.
type Keeper struct {
	cdc codec.BinaryCodec

	hyperlaneKeeper types.HyperlaneKeeper
	msgRouter       types.MessageRouter

	// Routers: <id> -> HypInterchainAccount
	ICARouters collections.Map[uint64, types.InterchainAccountsRouter]
	// RemoteRouters: <id> <domain> -> Router
	EnrolledRouters collections.Map[collections.Pair[uint64, uint32], warptypes.RemoteRouter]
}

// NewKeeper creates a new forwarding Keeper instance.
func NewKeeper(cdc codec.Codec, storeService storetypes.KVStoreService, hyperlaneKeeper types.HyperlaneKeeper, msgRouter types.MessageRouter) Keeper {
	keeper := Keeper{
		cdc:             cdc,
		hyperlaneKeeper: hyperlaneKeeper,
		msgRouter:       msgRouter,
	}

	appRouter := hyperlaneKeeper.AppRouter()
	appRouter.RegisterModule(255, &keeper)

	return keeper
}

// Exists implements [util.HyperlaneApp].
func (k *Keeper) Exists(ctx context.Context, recipient util.HexAddress) (bool, error) {
	return k.ICARouters.Has(ctx, recipient.GetInternalId())
}

// Handle implements [util.HyperlaneApp].
func (k *Keeper) Handle(ctx context.Context, mailboxId util.HexAddress, message util.HyperlaneMessage) error {
	icaRouter, err := k.ICARouters.Get(ctx, message.Recipient.GetInternalId())
	if err != nil {
		return err
	}

	payload, err := types.ParseInterchainAccountsPayload(message.Body)
	if err != nil {
		return err
	}

	if icaRouter.OriginMailbox != mailboxId {
		return fmt.Errorf("invalid origin mailbox address")
	}

	remoteRouter, err := k.EnrolledRouters.Get(ctx, collections.Join(message.Recipient.GetInternalId(), message.Origin))
	if err != nil {
		return fmt.Errorf("no enrolled router found for origin %d", message.Origin)
	}

	if message.Sender.String() != strings.ToLower(remoteRouter.ReceiverContract) {
		return fmt.Errorf("invalid receiver contract")
	}

	// TODO: If the ICA message contains the user salt as the warp message id, then we can assert that the warp message
	// id exists (must exist) in the core hyperlane messages keyset prior to processing this message.
	// ref: https://github.com/bcp-innovations/hyperlane-cosmos/blob/main/x/core/keeper/logic_message.go#L50-L61

	return k.handlePayload(icaRouter, payload)
}

func (k *Keeper) handlePayload(icaRouter types.InterchainAccountsRouter, payload types.InterchainAccountsPayload) error {
	panic("unimplemented")
}

// ReceiverIsmId implements [util.HyperlaneApp].
func (k *Keeper) ReceiverIsmId(ctx context.Context, recipient util.HexAddress) (*util.HexAddress, error) {
	token, err := k.ICARouters.Get(ctx, recipient.GetInternalId())
	if err != nil {
		return nil, fmt.Errorf("TODO: use typed error")
	}

	if token.IsmId == nil {
		mailbox, err := k.hyperlaneKeeper.GetMailbox(ctx, token.OriginMailbox)
		if err != nil {
			return nil, err
		}
		return &mailbox.DefaultIsm, nil
	}

	return token.IsmId, nil
}
