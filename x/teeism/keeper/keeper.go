package keeper

import (
	"context"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ util.InterchainSecurityModule = (*Keeper)(nil)

// Keeper implements the InterchainSecurityModule interface required by the
// Hyperlane ISM Router.
type Keeper struct {
	isms collections.Map[uint64, types.InterchainSecurityModule]
	// roots holds every origin tree root each ism has ever attested, keyed by
	// (ism, root). A delivery proof may be against any of them.
	roots  collections.KeySet[collections.Pair[uint64, []byte]]
	schema collections.Schema

	coreKeeper types.HyperlaneKeeper
	authority  string
}

// NewKeeper creates and returns a new teeism module Keeper.
func NewKeeper(cdc codec.Codec, storeService corestore.KVStoreService, hyperlaneKeeper types.HyperlaneKeeper, authority string) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	isms := collections.NewMap(sb, types.IsmsKeyPrefix, "isms", collections.Uint64Key, codec.CollValue[types.InterchainSecurityModule](cdc))
	roots := collections.NewKeySet(sb, types.RootsKeyPrefix, "roots", collections.PairKeyCodec(collections.Uint64Key, collections.BytesKey))

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	keeper := &Keeper{
		coreKeeper: hyperlaneKeeper,
		isms:       isms,
		roots:      roots,
		schema:     schema,
		authority:  authority,
	}

	router := hyperlaneKeeper.IsmRouter()
	router.RegisterModule(types.ModuleTypeTeeISM, keeper)

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
//
// A message is delivered with its place in the origin tree: the metadata names an
// attested root, the leaf's index and the 32 siblings that fold the message id up
// to that root (types.DeliveryMetadata). The attestation vouched for the root;
// the branch places the message under it. Nothing is written: the mailbox already
// refuses to process a message id twice.
func (k *Keeper) Verify(ctx context.Context, ismId util.HexAddress, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	ism, err := k.isms.Get(ctx, ismId.GetInternalId())
	if err != nil {
		return false, errorsmod.Wrap(types.ErrIsmNotFound, err.Error())
	}

	k.Logger(ctx).Info("processing message", "id", message.Id().String(), "ism", ism.Id.String())

	proof, err := types.DecodeDeliveryMetadata(metadata)
	if err != nil {
		k.Logger(ctx).Info("refusing message", "id", message.Id().String(), "reason", err.Error())
		return false, nil
	}

	attested, err := k.roots.Has(ctx, collections.Join(ism.Id.GetInternalId(), proof.Root[:]))
	if err != nil {
		return false, err
	}
	if !attested {
		return false, nil
	}

	var leaf [32]byte
	copy(leaf[:], message.Id().Bytes())
	return proof.Proves(leaf), nil
}
