package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/codec"
)

type Keeper struct {
	cdc           codec.BinaryCodec
	Schema        collections.Schema
	Params        collections.Item[types.Params]
	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	warpKeeper    *warpkeeper.Keeper
	authority     string
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	warpKeeper *warpkeeper.Keeper,
	authority string,
) Keeper {
	if warpKeeper == nil {
		panic("warpKeeper cannot be nil")
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:           cdc,
		Params:        collections.NewItem(sb, types.ParamsPrefix, "params", codec.CollValue[types.Params](cdc)),
		accountKeeper: accountKeeper,
		bankKeeper:    bankKeeper,
		warpKeeper:    warpKeeper,
		authority:     authority,
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

func (k Keeper) GetParams(ctx context.Context) (types.Params, error) {
	return k.Params.Get(ctx)
}

func (k Keeper) SetParams(ctx context.Context, params types.Params) error {
	return k.Params.Set(ctx, params)
}

// FindHypTokenByDenom finds the HypToken for a given bank denom and destination domain.
// For TIA (utia), it searches warp tokens with OriginDenom="utia" that have a route to destDomain.
// For synthetic tokens (hyperlane/...), it looks up the token by ID.
func (k Keeper) FindHypTokenByDenom(ctx context.Context, denom string, destDomain uint32) (warptypes.HypToken, error) {
	switch {
	case denom == "utia":
		return k.findTIACollateralTokenForDomain(ctx, destDomain)
	case strings.HasPrefix(denom, "hyperlane/"):
		return k.findSyntheticToken(ctx, strings.TrimPrefix(denom, "hyperlane/"))
	default:
		return warptypes.HypToken{}, types.ErrUnsupportedToken
	}
}

// findTIACollateralTokenForDomain finds the TIA collateral token with a route to the destination domain.
// It iterates all warp tokens to find one with OriginDenom="utia", TokenType=COLLATERAL,
// and an enrolled router for the destination domain.
func (k Keeper) findTIACollateralTokenForDomain(ctx context.Context, destDomain uint32) (warptypes.HypToken, error) {
	iter, err := k.warpKeeper.HypTokens.Iterate(ctx, nil)
	if err != nil {
		return warptypes.HypToken{}, fmt.Errorf("failed to iterate warp tokens: %w", err)
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		token, err := iter.Value()
		if err != nil {
			continue
		}
		// Find TIA collateral token with route to destination
		if token.OriginDenom == "utia" && token.TokenType == warptypes.HYP_TOKEN_TYPE_COLLATERAL {
			hasRoute, _ := k.HasEnrolledRouter(ctx, token.Id, destDomain)
			if hasRoute {
				return token, nil
			}
		}
	}
	return warptypes.HypToken{}, fmt.Errorf("%w: no TIA collateral route to domain %d", types.ErrNoWarpRoute, destDomain)
}

func (k Keeper) findSyntheticToken(ctx context.Context, tokenIdHex string) (warptypes.HypToken, error) {
	return k.getTokenByHex(ctx, tokenIdHex)
}

func (k Keeper) getTokenByHex(ctx context.Context, tokenIdHex string) (warptypes.HypToken, error) {
	tokenId, err := util.DecodeHexAddress(tokenIdHex)
	if err != nil {
		return warptypes.HypToken{}, fmt.Errorf("%w: invalid token ID %q", types.ErrUnsupportedToken, tokenIdHex)
	}
	token, err := k.warpKeeper.HypTokens.Get(ctx, tokenId.GetInternalId())
	if err != nil {
		return warptypes.HypToken{}, fmt.Errorf("token %s not found: %w", tokenIdHex, err)
	}
	return token, nil
}

func (k Keeper) HasEnrolledRouter(ctx context.Context, tokenId util.HexAddress, destDomain uint32) (bool, error) {
	return k.warpKeeper.EnrolledRouters.Has(ctx, collections.Join(tokenId.GetInternalId(), destDomain))
}

func (k Keeper) ExecuteWarpTransfer(
	ctx sdk.Context,
	token warptypes.HypToken,
	sender string,
	destDomain uint32,
	destRecipient util.HexAddress,
	amount math.Int,
) (util.HexAddress, error) {
	gasLimit := math.ZeroInt()
	maxFee := sdk.NewCoin("utia", math.ZeroInt())

	switch token.TokenType {
	case warptypes.HYP_TOKEN_TYPE_SYNTHETIC:
		return k.warpKeeper.RemoteTransferSynthetic(ctx, token, sender, destDomain, destRecipient, amount, nil, gasLimit, maxFee, nil)
	case warptypes.HYP_TOKEN_TYPE_COLLATERAL:
		return k.warpKeeper.RemoteTransferCollateral(ctx, token, sender, destDomain, destRecipient, amount, nil, gasLimit, maxFee, nil)
	default:
		return util.HexAddress{}, types.ErrUnsupportedToken
	}
}
