package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/codec"
)

var _ util.InterchainSecurityModule = (*Keeper)(nil)

// Keeper implements the InterchainSecurityModule interface required by the Hyperlane ISM Router.
type Keeper struct {
	isms   collections.Map[uint64, types.ZKExecutionISM]
	schema collections.Schema

	coreKeeper    types.HyperlaneKeeper
	stakingKeeper types.StakingKeeper
	authority     string
}

// NewKeeper creates and returns a new zkism module Keeper.
func NewKeeper(cdc codec.Codec, storeService corestore.KVStoreService, hyperlaneKeeper types.HyperlaneKeeper, stakingKeeper types.StakingKeeper, authority string) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	isms := collections.NewMap(sb, types.IsmsKeyPrefix, "isms", collections.Uint64Key, codec.CollValue[types.ZKExecutionISM](cdc))

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	keeper := &Keeper{
		coreKeeper:    hyperlaneKeeper,
		stakingKeeper: stakingKeeper,
		isms:          isms,
		schema:        schema,
		authority:     authority,
	}

	router := hyperlaneKeeper.IsmRouter()
	router.RegisterModule(types.InterchainSecurityModuleTypeZKExecution, keeper)

	return keeper
}

// GetHeader retrieves the block header for the provided height using x/staking HistoricalInfo.
// HistoricalInfo maintains a window of a historical block headers stored within state (default: 10000).
// See DefaultHistoricalEntries in x/staking params.
func (k *Keeper) GetHeader(ctx context.Context, height uint64) (coretypes.Header, error) {
	historicalInfo, err := k.stakingKeeper.GetHistoricalInfo(ctx, int64(height))
	if err != nil {
		return coretypes.Header{}, err
	}

	header, err := coretypes.HeaderFromProto(&historicalInfo.Header)
	if err != nil {
		return coretypes.Header{}, err
	}

	return header, nil
}

// Exists implements hyperlane util.InterchainSecurityModule.
func (k *Keeper) Exists(ctx context.Context, ismId util.HexAddress) (bool, error) {
	return k.isms.Has(ctx, ismId.GetInternalId())
}

// Verify implements hyperlane util.InterchainSecurityModule.
func (k *Keeper) Verify(ctx context.Context, ismId util.HexAddress, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	ism, err := k.isms.Get(ctx, ismId.GetInternalId())
	if err != nil {
		return false, err
	}

	meta, err := types.NewZkExecutionISMMetadata(metadata)
	if err != nil {
		return false, err
	}

	// TODO: add celestia height to the ism metadata struct
	if err := k.validatePublicValues(ctx, 0, ism, meta.PublicValues); err != nil {
		return false, err
	}

	return ism.Verify(ctx, metadata, message)
}

func (k *Keeper) validatePublicValues(ctx context.Context, height uint64, ism types.ZKExecutionISM, publicValues types.PublicValues) error {
	header, err := k.GetHeader(ctx, height)
	if err != nil {
		return fmt.Errorf("failed to get header for height %d: %w", height, err)
	}

	if !bytes.Equal(header.Hash().Bytes(), publicValues.CelestiaHeaderHash[:]) {
		return fmt.Errorf("invalid header hash, expected %x, got %x", header.Hash().Bytes(), publicValues.CelestiaHeaderHash[:])
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
