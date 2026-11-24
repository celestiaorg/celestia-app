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
	"github.com/celestiaorg/celestia-app/v12/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	lru "github.com/hashicorp/golang-lru/v2"
)

// verifierCacheSize bounds the parsed verifying keys kept; ISMs on the same SP1 release share one key.
const verifierCacheSize = 128

var _ util.InterchainSecurityModule = (*Keeper)(nil)

// Keeper implements the InterchainSecurityModule interface required by the Hyperlane ISM Router.
type Keeper struct {
	isms             collections.Map[uint64, types.InterchainSecurityModule]
	treeStates       collections.Map[uint64, types.MerkleTreeState]
	stateRootHistory rootHistory
	treeStateHistory rootHistory
	schema           collections.Schema

	// verifiers memoizes parsed Groth16 verifying keys by their bytes; a hit only saves the parse's pairing.
	verifiers *lru.Cache[string, *types.SP1Groth16Verifier]

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

	verifiers, err := lru.New[string, *types.SP1Groth16Verifier](verifierCacheSize)
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
		verifiers:        verifiers,
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

// verifier returns the verifier for vkey, parsing the key only when it is not cached. Parse errors
// are not cached.
func (k *Keeper) verifier(vkey []byte) (*types.SP1Groth16Verifier, error) {
	key := string(vkey)
	if v, ok := k.verifiers.Get(key); ok {
		return v, nil
	}
	v, err := types.NewSP1Groth16Verifier(vkey)
	if err != nil {
		return nil, err
	}
	k.verifiers.Add(key, v)
	return v, nil
}

// Exists implements hyperlane util.InterchainSecurityModule.
func (k *Keeper) Exists(ctx context.Context, ismId util.HexAddress) (bool, error) {
	return k.isms.Has(ctx, ismId.GetInternalId())
}

// Verify implements the util.InterchainSecurityModule interface. The metadata must contain a merkle proof of the
// message id against the latest proven merkle tree root or a root in the ISM's tree state history.
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

	// The leaf is computed from the message, so only a message in the proven tree can match the root.
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
