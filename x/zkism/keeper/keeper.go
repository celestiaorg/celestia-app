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
	isms        collections.Map[uint64, types.InterchainSecurityModule]
	submissions collections.Map[uint64, bool]
	treeStates  collections.Map[uint64, types.MerkleTreeState]
	schema      collections.Schema

	coreKeeper types.HyperlaneKeeper
	authority  string
}

// NewKeeper creates and returns a new zkism module Keeper.
func NewKeeper(cdc codec.Codec, storeService corestore.KVStoreService, hyperlaneKeeper types.HyperlaneKeeper, authority string) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	isms := collections.NewMap(sb, types.IsmsKeyPrefix, "isms", collections.Uint64Key, codec.CollValue[types.InterchainSecurityModule](cdc))
	submissions := collections.NewMap(sb, types.MessageProofSubmittedPrefix, "message_proof_submitted", collections.Uint64Key, collections.BoolValue)
	treeStates := collections.NewMap(sb, types.MerkleTreeStateKeyPrefix, "merkle_tree_states", collections.Uint64Key, codec.CollValue[types.MerkleTreeState](cdc))

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	keeper := &Keeper{
		coreKeeper:  hyperlaneKeeper,
		isms:        isms,
		submissions: submissions,
		treeStates:  treeStates,
		schema:      schema,
		authority:   authority,
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
// message id against the source chain merkle tree root most recently proven via SubmitMessages.
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

	treeState, err := k.treeStates.Get(ctx, ism.Id.GetInternalId())
	if errors.Is(err, collections.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if proof.Index >= treeState.Count {
		return false, nil
	}

	// The leaf is always message.Id(), computed here rather than supplied, and BranchRoot always hashes TreeDepth
	// levels, so only a message that is a leaf of the proven tree can match the root.
	root := util.BranchRoot(message.Id(), proof.Branch, proof.Index)
	return bytes.Equal(root[:], treeState.Root), nil
}
