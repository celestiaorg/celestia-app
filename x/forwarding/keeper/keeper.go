package keeper

import (
	"context"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/codec"
)

// Keeper is the forwarding module keeper
type Keeper struct {
	cdc codec.BinaryCodec

	// Schema for collections
	Schema collections.Schema

	// Params storage
	Params collections.Item[types.Params]

	// Dependencies
	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	// warpKeeper is the concrete warp keeper type (not an interface) because:
	// - hyperlane-cosmos exposes state via public collections.Map fields (HypTokens, EnrolledRouters)
	// - Go interfaces cannot expose struct fields, only methods
	// - We wrap field access in helper methods: FindHypTokenByDenom, HasEnrolledRouter
	// See types/expected_keepers.go for documentation of the warp keeper interface.
	warpKeeper *warpkeeper.Keeper
}

// NewKeeper creates a new forwarding Keeper
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	warpKeeper *warpkeeper.Keeper,
) Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:           cdc,
		Params:        collections.NewItem(sb, types.ParamsPrefix, "params", codec.CollValue[types.Params](cdc)),
		accountKeeper: accountKeeper,
		bankKeeper:    bankKeeper,
		warpKeeper:    warpKeeper,
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// GetParams returns the current module params
func (k Keeper) GetParams(ctx context.Context) (types.Params, error) {
	return k.Params.Get(ctx)
}

// SetParams sets the module params
func (k Keeper) SetParams(ctx context.Context, params types.Params) error {
	return k.Params.Set(ctx, params)
}

// DeriveForwardingAddress derives the forwarding address for given parameters
func (k Keeper) DeriveForwardingAddress(destDomain uint32, destRecipient []byte) sdk.AccAddress {
	return types.DeriveForwardingAddress(destDomain, destRecipient)
}

// FindHypTokenByDenom finds the HypToken for a given denom
// For utia, returns the TIA collateral token (read from params)
// For hyperlane/{id}, parses the token ID from the denom
func (k Keeper) FindHypTokenByDenom(ctx context.Context, denom string) (warptypes.HypToken, error) {
	// TIA is the only collateral token on Celestia
	if denom == "utia" {
		// Get TIA token ID from params
		params, err := k.GetParams(ctx)
		if err != nil {
			return warptypes.HypToken{}, err
		}
		if params.TiaCollateralTokenId == "" {
			return warptypes.HypToken{}, types.ErrUnsupportedToken
		}
		tiaTokenId, err := util.DecodeHexAddress(params.TiaCollateralTokenId)
		if err != nil {
			return warptypes.HypToken{}, types.ErrUnsupportedToken
		}
		return k.warpKeeper.HypTokens.Get(ctx, tiaTokenId.GetInternalId())
	}

	// Synthetic tokens have denom format: hyperlane/{hex-token-id}
	if strings.HasPrefix(denom, "hyperlane/") {
		tokenIdHex := strings.TrimPrefix(denom, "hyperlane/")
		tokenId, err := util.DecodeHexAddress(tokenIdHex)
		if err != nil {
			return warptypes.HypToken{}, types.ErrUnsupportedToken
		}
		return k.warpKeeper.HypTokens.Get(ctx, tokenId.GetInternalId())
	}

	return warptypes.HypToken{}, types.ErrUnsupportedToken
}

// HasEnrolledRouter checks if a warp route exists for a token to a destination domain
func (k Keeper) HasEnrolledRouter(ctx context.Context, tokenId util.HexAddress, destDomain uint32) (bool, error) {
	// Access the EnrolledRouters collection field directly from the concrete warp keeper
	return k.warpKeeper.EnrolledRouters.Has(ctx, collections.Join(tokenId.GetInternalId(), destDomain))
}

// ExecuteWarpTransfer executes a warp transfer for the given token
func (k Keeper) ExecuteWarpTransfer(
	ctx sdk.Context,
	token warptypes.HypToken,
	sender string,
	destDomain uint32,
	destRecipient util.HexAddress,
	amount math.Int,
) (util.HexAddress, error) {
	// Use gasLimit=0 to use router's configured default
	gasLimit := math.ZeroInt()
	// Max fee for relaying - using zero for now (TODO: make configurable)
	maxFee := sdk.NewCoin("utia", math.ZeroInt())

	switch token.TokenType {
	case warptypes.HYP_TOKEN_TYPE_SYNTHETIC:
		return k.warpKeeper.RemoteTransferSynthetic(
			ctx,
			token,
			sender,
			destDomain,
			destRecipient,
			amount,
			nil, // customHookId
			gasLimit,
			maxFee,
			nil, // customHookMetadata
		)
	case warptypes.HYP_TOKEN_TYPE_COLLATERAL:
		return k.warpKeeper.RemoteTransferCollateral(
			ctx,
			token,
			sender,
			destDomain,
			destRecipient,
			amount,
			nil, // customHookId
			gasLimit,
			maxFee,
			nil, // customHookMetadata
		)
	default:
		return util.HexAddress{}, types.ErrUnsupportedToken
	}
}
