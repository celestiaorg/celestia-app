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

func (k Keeper) FindHypTokenByDenom(ctx context.Context, denom string) (warptypes.HypToken, error) {
	switch {
	case denom == "utia":
		return k.findTIACollateralToken(ctx)
	case strings.HasPrefix(denom, "hyperlane/"):
		return k.findSyntheticToken(ctx, strings.TrimPrefix(denom, "hyperlane/"))
	default:
		return warptypes.HypToken{}, types.ErrUnsupportedToken
	}
}

func (k Keeper) findTIACollateralToken(ctx context.Context) (warptypes.HypToken, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return warptypes.HypToken{}, fmt.Errorf("failed to get params: %w", err)
	}
	if params.TiaCollateralTokenId == "" {
		return warptypes.HypToken{}, fmt.Errorf("%w: TiaCollateralTokenId not configured", types.ErrUnsupportedToken)
	}
	return k.getTokenByHex(ctx, params.TiaCollateralTokenId)
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
