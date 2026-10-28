package keeper

import (
	"bytes"
	"context"
	"errors"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ util.InterchainSecurityModule = (*Keeper)(nil)

// Keeper implements the InterchainSecurityModule interface required by the Hyperlane ISM Router.
type Keeper struct {
	isms             collections.Map[uint64, types.InterchainSecurityModule]
	treeStates       collections.Map[uint64, types.MerkleTreeState]
	stateRootHistory rootHistory
	treeStateHistory rootHistory
	schema           collections.Schema

	coreKeeper types.HyperlaneKeeper
	authority  string
}

// NewKeeper creates and returns a new zkism module Keeper.
func NewKeeper(cdc codec.Codec, storeService corestore.KVStoreService, hyperlaneKeeper types.HyperlaneKeeper, authority string) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	isms := collections.NewMap(sb, types.IsmsKeyPrefix, "isms", collections.Uint64Key, codec.CollValue[types.InterchainSecurityModule](cdc))
	treeStates := collections.NewMap(sb, types.MerkleTreeStateKeyPrefix, "merkle_tree_states", collections.Uint64Key, codec.CollValue[types.MerkleTreeState](cdc))
	historyKey := collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)
	stateRootHistory := collections.NewMap(sb, types.StateRootHistoryPrefix, "state_root_history", historyKey, codec.CollValue[types.RootHistoryEntry](cdc))
	treeStateHistory := collections.NewMap(sb, types.TreeStateHistoryPrefix, "tree_state_history", historyKey, codec.CollValue[types.RootHistoryEntry](cdc))

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	keeper := &Keeper{
		coreKeeper:       hyperlaneKeeper,
		isms:             isms,
		treeStates:       treeStates,
		stateRootHistory: stateRootHistory,
		treeStateHistory: treeStateHistory,
		schema:           schema,
		authority:        authority,
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

// Verify implements the util.InterchainSecurityModule interface. The metadata must contain a merkle proof of the
// message id against the source chain merkle tree root most recently proven via SubmitMessages, or against one of the
// replaced tree roots still kept in the ISM's root history. The source tree is append-only, so an older root never
// authorizes a message a newer root does not.
// Replay protection is provided by the Hyperlane mailbox, which rejects message ids it has already received.
func (k *Keeper) Verify(ctx context.Context, ismId util.HexAddress, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	ism, err := k.isms.Get(ctx, ismId.GetInternalId())
	if err != nil {
		return false, errorsmod.Wrap(types.ErrIsmNotFound, err.Error())
	}

	k.Logger(ctx).Info("processing message", "id", message.Id().String(), "ism", ism.Id.String())

	proof, err := types.ParseMerkleProofMetadata(metadata)
	if err != nil {
		return false, errorsmod.Wrap(types.ErrInvalidMetadata, err.Error())
	}

	// The leaf is always message.Id(), computed here rather than supplied, and BranchRoot always hashes TreeDepth
	// levels, so only a message that is a leaf of the proven tree can match the root.
	root := util.BranchRoot(message.Id(), proof.Branch, proof.Index)

	treeState, err := k.treeStates.Get(ctx, ism.Id.GetInternalId())
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return false, err
	}
	if err == nil && proof.Index < treeState.Count && bytes.Equal(root[:], treeState.Root) {
		return true, nil
	}

	return historyContains(ctx, k.treeStateHistory, ism.Id.GetInternalId(), func(entry types.RootHistoryEntry) bool {
		return proof.Index < entry.Count && bytes.Equal(root[:], entry.Root)
	})
}
