package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper maintains state for the forwarding module.
type Keeper struct {
	cdc codec.BinaryCodec

	hypKeeper types.HyperlaneKeeper
	msgRouter types.MessageRouter

	// InterchainAccountsRouters: <id> -> InterchainAccountsRouter
	InterchainAccountsRouters collections.Map[uint64, types.InterchainAccountsRouter]
	// RemoteRouters: <id> <domain> -> RemoteRouter
	RemoteRouters collections.Map[collections.Pair[uint64, uint32], types.RemoteRouter]
}

// NewKeeper creates a new forwarding Keeper instance.
func NewKeeper(cdc codec.Codec, storeService storetypes.KVStoreService, hypKeeper types.HyperlaneKeeper, msgRouter types.MessageRouter) Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	keeper := Keeper{
		cdc:                       cdc,
		hypKeeper:                 hypKeeper,
		msgRouter:                 msgRouter,
		InterchainAccountsRouters: collections.NewMap(sb, types.RoutersKeyPrefix, "routers", collections.Uint64Key, codec.CollValue[types.InterchainAccountsRouter](cdc)),
		RemoteRouters:             collections.NewMap(sb, types.RemoteRoutersKeyPrefix, "remote_routers", collections.PairKeyCodec(collections.Uint64Key, collections.Uint32Key), codec.CollValue[types.RemoteRouter](cdc)),
	}

	appRouter := hypKeeper.AppRouter()
	appRouter.RegisterModule(types.HyperlaneModuleID, &keeper)

	return keeper
}

// Logger returns the module logger extracted using the sdk context.
func (k *Keeper) Logger(ctx context.Context) log.Logger {
	return sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/"+types.ModuleName)
}

// Exists implements [util.HyperlaneApp].
func (k *Keeper) Exists(ctx context.Context, recipient util.HexAddress) (bool, error) {
	return k.InterchainAccountsRouters.Has(ctx, recipient.GetInternalId())
}

// Handle implements [util.HyperlaneApp].
func (k *Keeper) Handle(ctx context.Context, mailboxId util.HexAddress, message util.HyperlaneMessage) error {
	k.Logger(ctx).Info("Processing interchain accounts msg from Hyperlane core")

	router, err := k.InterchainAccountsRouters.Get(ctx, message.Recipient.GetInternalId())
	if err != nil {
		return err
	}

	payload, err := types.ParseInterchainAccountsPayload(message.Body)
	if err != nil {
		return err
	}

	k.Logger(ctx).Info("ICA Payload info", payload)

	if router.OriginMailbox != mailboxId {
		return fmt.Errorf("invalid origin mailbox address")
	}

	remoteRouter, err := k.RemoteRouters.Get(ctx, collections.Join(message.Recipient.GetInternalId(), message.Origin))
	if err != nil {
		return fmt.Errorf("no enrolled router found for origin %d", message.Origin)
	}

	if message.Sender.String() != strings.ToLower(remoteRouter.ReceiverContract) {
		return fmt.Errorf("invalid receiver contract")
	}

	// TODO: If the ICA message contains the user salt as the warp message id, then we can assert that the warp message
	// id exists (must exist) in the core hyperlane messages keyset prior to processing this message.
	// ref: https://github.com/bcp-innovations/hyperlane-cosmos/blob/main/x/core/keeper/logic_message.go#L50-L61

	return k.handlePayload(ctx, router, payload)
}

func (k *Keeper) handlePayload(ctx context.Context, icaRouter types.InterchainAccountsRouter, payload types.InterchainAccountsPayload) error {
	k.Logger(ctx).Info("Successfully processed interchain accounts msg from Hyperlane core")
	return nil
}

// ReceiverIsmId implements [util.HyperlaneApp].
func (k *Keeper) ReceiverIsmId(ctx context.Context, recipient util.HexAddress) (*util.HexAddress, error) {
	router, err := k.InterchainAccountsRouters.Get(ctx, recipient.GetInternalId())
	if err != nil {
		return nil, fmt.Errorf("TODO: use typed error")
	}

	if router.IsmId == nil {
		mailbox, err := k.hypKeeper.GetMailbox(ctx, router.OriginMailbox)
		if err != nil {
			return nil, err
		}
		return &mailbox.DefaultIsm, nil
	}

	return router.IsmId, nil
}
