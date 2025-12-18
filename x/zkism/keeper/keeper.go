package keeper

import (
	"bytes"
	"context"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ util.InterchainSecurityModule = (*Keeper)(nil)

// Keeper implements the InterchainSecurityModule interface required by the Hyperlane ISM Router.
type Keeper struct {
	isms     collections.Map[uint64, types.InterchainSecurityModule]
	messages collections.KeySet[collections.Pair[uint64, []byte]]
	schema   collections.Schema

	coreKeeper types.HyperlaneKeeper
	authority  string
}

// NewKeeper creates and returns a new zkism module Keeper.
func NewKeeper(cdc codec.Codec, storeService corestore.KVStoreService, hyperlaneKeeper types.HyperlaneKeeper, authority string) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	isms := collections.NewMap(sb, types.IsmsKeyPrefix, "isms", collections.Uint64Key, codec.CollValue[types.InterchainSecurityModule](cdc))
	messages := collections.NewKeySet(sb, types.MessageKeyPrefix, "messages", collections.PairKeyCodec(collections.Uint64Key, collections.BytesKey))

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	keeper := &Keeper{
		coreKeeper: hyperlaneKeeper,
		isms:       isms,
		messages:   messages,
		schema:     schema,
		authority:  authority,
	}

	router := hyperlaneKeeper.IsmRouter()
	router.RegisterModule(types.ModuleTypeZkISM, keeper)

	return keeper
}

// Logger returns the module logger extracted using the sdk context.
func (k *Keeper) Logger(ctx context.Context) log.Logger {
	return sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/"+types.ModuleName)
}

// Exists implements hyperlane util.InterchainSecurityModule.
func (k *Keeper) Exists(ctx context.Context, ismId util.HexAddress) (bool, error) {
	return k.isms.Has(ctx, ismId.GetInternalId())
}

// Verify implements hyperlane util.InterchainSecurityModule.
func (k *Keeper) Verify(ctx context.Context, ismId util.HexAddress, _ []byte, message util.HyperlaneMessage) (bool, error) {
	ism, err := k.isms.Get(ctx, ismId.GetInternalId())
	if err != nil {
		return false, errorsmod.Wrap(types.ErrIsmNotFound, err.Error())
	}

	k.Logger(ctx).Info("processing message", "id", message.Id().String(), "ism", ism.Id.String())

	authorized, err := k.messages.Has(ctx, collections.Join(ism.Id.GetInternalId(), message.Id().Bytes()))
	if err != nil {
		return false, err
	}

	if authorized {
		if err := k.messages.Remove(ctx, collections.Join(ism.Id.GetInternalId(), message.Id().Bytes())); err != nil {
			return false, err
		}
	}

	return authorized, nil
}

func (k *Keeper) validatePublicValues(ctx context.Context, ism types.InterchainSecurityModule, publicValues types.StateTransitionValues) error {
	if len(publicValues.State) < 32 || len(publicValues.NewState) < 32 {
		return errorsmod.Wrapf(types.ErrInvalidState, "state must be at least 32 bytes")
	}

	if !bytes.Equal(ism.State, publicValues.State) {
		return errorsmod.Wrapf(types.ErrInvalidState, "expected %x, got %x", ism.State, publicValues.State)
	}

	return nil
}
