package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/codec"
)

var _ util.InterchainSecurityModule = (*Keeper)(nil)

// Keeper implements the InterchainSecurityModule interface required by the Hyperlane ISM Router.
type Keeper struct {
	headers collections.Map[uint64, []byte]
	isms    collections.Map[uint64, types.ZKExecutionISM]
	params  collections.Item[types.Params]
	schema  collections.Schema

	coreKeeper types.HyperlaneKeeper
	authority  string
}

// NewKeeper creates and returns a new zkism module Keeper.
func NewKeeper(cdc codec.Codec, storeService corestore.KVStoreService, hyperlaneKeeper types.HyperlaneKeeper, authority string) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	headers := collections.NewMap(sb, types.HeadersKeyPrefix, "headers", collections.Uint64Key, collections.BytesValue)
	isms := collections.NewMap(sb, types.IsmsKeyPrefix, "isms", collections.Uint64Key, codec.CollValue[types.ZKExecutionISM](cdc))
	params := collections.NewItem(sb, types.ParamsKeyPrefix, "params", codec.CollValue[types.Params](cdc))

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	keeper := &Keeper{
		coreKeeper: hyperlaneKeeper,
		headers:    headers,
		isms:       isms,
		params:     params,
		schema:     schema,
		authority:  authority,
	}

	router := hyperlaneKeeper.IsmRouter()
	router.RegisterModule(types.InterchainSecurityModuleTypeZKExecution, keeper)

	return keeper
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
// TODO: Refactor this method to check for authorized messages in kv store.
func (k *Keeper) Verify(ctx context.Context, ismId util.HexAddress, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	ism, err := k.isms.Get(ctx, ismId.GetInternalId())
	if err != nil {
		return false, err
	}

	meta, err := types.NewZkExecutionISMMetadata(metadata)
	if err != nil {
		return false, err
	}

	if err := k.validatePublicValues(ctx, meta.Height, ism, meta.PublicValues); err != nil {
		return false, err
	}

	verified, err := ism.Verify(ctx, metadata, message)
	if err != nil {
		return false, err
	}

	if verified {
		ism.Height = meta.PublicValues.NewHeight
		ism.StateRoot = meta.PublicValues.NewStateRoot[:]

		if err := k.isms.Set(ctx, ismId.GetInternalId(), ism); err != nil {
			return false, err
		}
	}

	return verified, nil
}

func (k *Keeper) validatePublicValues(ctx context.Context, height uint64, ism types.ZKExecutionISM, publicValues types.PublicValues) error {
	headerHash, err := k.GetHeaderHash(ctx, height)
	if err != nil {
		return fmt.Errorf("failed to get header for height %d: %w", height, err)
	}

	if !bytes.Equal(headerHash, publicValues.CelestiaHeaderHash[:]) {
		return fmt.Errorf("invalid header hash, expected %x, got %x", headerHash, publicValues.CelestiaHeaderHash[:])
	}

	if !bytes.Equal(publicValues.TrustedStateRoot[:], ism.StateRoot) {
		return fmt.Errorf("invalid trusted state root: expected %x, got %x", ism.StateRoot, publicValues.TrustedStateRoot)
	}

	if publicValues.TrustedHeight != ism.Height {
		return fmt.Errorf("invalid trusted height: expected %d, got %d", ism.Height, publicValues.TrustedHeight)
	}

	if !bytes.Equal(publicValues.Namespace[:], ism.Namespace) {
		return fmt.Errorf("invalid namespace: expected %x, got %x", ism.Namespace, publicValues.Namespace)
	}

	if !bytes.Equal(publicValues.PublicKey[:], ism.SequencerPublicKey) {
		return fmt.Errorf("invalid sequencer public key: expected %x, got %x", ism.SequencerPublicKey, publicValues.PublicKey)
	}

	return nil
}
