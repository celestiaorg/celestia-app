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

type Keeper struct {
	cdc           codec.BinaryCodec
	Schema        collections.Schema
	Params        collections.Item[types.Params]
	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	warpKeeper    *warpkeeper.Keeper
}

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

func (k Keeper) GetParams(ctx context.Context) (types.Params, error) {
	return k.Params.Get(ctx)
}

func (k Keeper) SetParams(ctx context.Context, params types.Params) error {
	return k.Params.Set(ctx, params)
}

func (k Keeper) DeriveForwardingAddress(destDomain uint32, destRecipient []byte) sdk.AccAddress {
	return types.DeriveForwardingAddress(destDomain, destRecipient)
}

func (k Keeper) FindHypTokenByDenom(ctx context.Context, denom string) (warptypes.HypToken, error) {
	if k.warpKeeper == nil {
		return warptypes.HypToken{}, types.ErrUnsupportedToken
	}

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

func (k Keeper) findSyntheticToken(ctx context.Context, tokenIdHex string) (warptypes.HypToken, error) {
	tokenId, err := util.DecodeHexAddress(tokenIdHex)
	if err != nil {
		return warptypes.HypToken{}, types.ErrUnsupportedToken
	}
	return k.warpKeeper.HypTokens.Get(ctx, tokenId.GetInternalId())
}

func (k Keeper) HasEnrolledRouter(ctx context.Context, tokenId util.HexAddress, destDomain uint32) (bool, error) {
	if k.warpKeeper == nil {
		return false, types.ErrUnsupportedToken
	}
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
	if k.warpKeeper == nil {
		return util.HexAddress{}, types.ErrUnsupportedToken
	}

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
