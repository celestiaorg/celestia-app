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
	headers  collections.Map[uint64, []byte]
	isms     collections.Map[uint64, types.ZKExecutionISM]
	messages collections.KeySet[[]byte]
	params   collections.Item[types.Params]
	schema   collections.Schema

	coreKeeper types.HyperlaneKeeper
	authority  string
}

// NewKeeper creates and returns a new zkism module Keeper.
func NewKeeper(cdc codec.Codec, storeService corestore.KVStoreService, hyperlaneKeeper types.HyperlaneKeeper, authority string) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	headers := collections.NewMap(sb, types.HeadersKeyPrefix, "headers", collections.Uint64Key, collections.BytesValue)
	isms := collections.NewMap(sb, types.IsmsKeyPrefix, "isms", collections.Uint64Key, codec.CollValue[types.ZKExecutionISM](cdc))
	messages := collections.NewKeySet(sb, types.MessageKeyPrefix, "messages", collections.BytesKey)
	params := collections.NewItem(sb, types.ParamsKeyPrefix, "params", codec.CollValue[types.Params](cdc))

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	keeper := &Keeper{
		coreKeeper: hyperlaneKeeper,
		headers:    headers,
		isms:       isms,
		messages:   messages,
		params:     params,
		schema:     schema,
		authority:  authority,
	}

	router := hyperlaneKeeper.IsmRouter()
	router.RegisterModule(types.InterchainSecurityModuleTypeZKExecution, keeper)

	return keeper
}

// Logger returns the module logger extracted using the sdk context.
func (k *Keeper) Logger(ctx context.Context) log.Logger {
	return sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/"+types.ModuleName)
}

// GetHeaderHash retrieves the block header hash for the provided height.
func (k *Keeper) GetHeaderHash(ctx context.Context, height uint64) ([]byte, error) {
	return k.headers.Get(ctx, height)
}

// GetMaxHeaderHashes returns the header hash retention policy parameter.
func (k *Keeper) GetMaxHeaderHashes(ctx context.Context) (uint32, error) {
	params, err := k.params.Get(ctx)
	return params.MaxHeaderHashes, err
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

	authorized, err := k.messages.Has(ctx, message.Id().Bytes())
	if err != nil {
		return false, err
	}

	if authorized {
		if err := k.messages.Remove(ctx, message.Id().Bytes()); err != nil {
			return false, err
		}
	}

	return authorized, nil
}

func (k *Keeper) validatePublicValues(ctx context.Context, ism types.ZKExecutionISM, publicValues types.EvExecutionPublicValues) error {
	if !bytes.Equal(ism.State, publicValues.State) {
		return errorsmod.Wrapf(types.ErrInvalidState, "expected %x, got %x", ism.State, publicValues.State)
	}

	return nil
}
