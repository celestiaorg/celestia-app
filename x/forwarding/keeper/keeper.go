// Package keeper implements the forwarding module keeper, which manages
// automatic cross-chain token forwarding via Hyperlane warp routes.
package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper manages forwarding module state and coordinates with Hyperlane warp for cross-chain transfers.
type Keeper struct {
	cdc             codec.BinaryCodec
	Schema          collections.Schema
	Params          collections.Item[types.Params]
	accountKeeper   types.AccountKeeper
	bankKeeper      types.BankKeeper
	warpKeeper      *warpkeeper.Keeper
	hyperlaneKeeper types.HyperlaneKeeper
	authority       string
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	warpKeeper *warpkeeper.Keeper,
	hyperlaneKeeper types.HyperlaneKeeper,
	authority string,
) Keeper {
	if accountKeeper == nil {
		panic("accountKeeper cannot be nil")
	}
	if bankKeeper == nil {
		panic("bankKeeper cannot be nil")
	}
	if warpKeeper == nil {
		panic("warpKeeper cannot be nil")
	}
	if hyperlaneKeeper == nil {
		panic("hyperlaneKeeper cannot be nil")
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:             cdc,
		Params:          collections.NewItem(sb, types.ParamsPrefix, "params", codec.CollValue[types.Params](cdc)),
		accountKeeper:   accountKeeper,
		bankKeeper:      bankKeeper,
		warpKeeper:      warpKeeper,
		hyperlaneKeeper: hyperlaneKeeper,
		authority:       authority,
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
	if err := params.Validate(); err != nil {
		return err
	}
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
		return k.getTokenById(ctx, strings.TrimPrefix(denom, "hyperlane/"))
	default:
		return warptypes.HypToken{}, types.ErrUnsupportedToken
	}
}

// findTIACollateralTokenForDomain finds the TIA collateral token with a route to the destination domain.
// It iterates all warp tokens to find one with OriginDenom="utia", TokenType=COLLATERAL,
// and an enrolled router for the destination domain.
//
// Note: This is O(n) where n = number of warp tokens. Optimization would require an index
// in the warp keeper mapping (denom, destDomain) -> tokenId, which is outside this module's scope.
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

func (k Keeper) getTokenById(ctx context.Context, tokenIdHex string) (warptypes.HypToken, error) {
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

// ExecuteWarpTransfer executes a Hyperlane warp transfer using the pre-computed IGP fee.
// The quotedFee must be provided by the caller (collected from the relayer in msg_server).
// This ensures only relayer-provided funds are used for IGP fees (no module-paid fallback).
func (k Keeper) ExecuteWarpTransfer(
	ctx sdk.Context,
	token warptypes.HypToken,
	sender string,
	destDomain uint32,
	destRecipient util.HexAddress,
	amount math.Int,
	quotedFee sdk.Coin,
) (util.HexAddress, error) {
	gasLimit := math.ZeroInt()

	switch token.TokenType {
	case warptypes.HYP_TOKEN_TYPE_SYNTHETIC:
		return k.warpKeeper.RemoteTransferSynthetic(ctx, token, sender, destDomain, destRecipient, amount, nil, gasLimit, quotedFee, nil)
	case warptypes.HYP_TOKEN_TYPE_COLLATERAL:
		return k.warpKeeper.RemoteTransferCollateral(ctx, token, sender, destDomain, destRecipient, amount, nil, gasLimit, quotedFee, nil)
	default:
		return util.HexAddress{}, types.ErrUnsupportedToken
	}
}

// QuoteIgpFee returns the IGP fee required for forwarding TIA to a destination domain.
// This is a convenience method for relayers to estimate fees before submitting MsgForward.
func (k Keeper) QuoteIgpFee(ctx context.Context, destDomain uint32) (sdk.Coin, error) {
	token, err := k.findTIACollateralTokenForDomain(ctx, destDomain)
	if err != nil {
		return sdk.Coin{}, err
	}
	return k.QuoteIgpFeeForToken(sdk.UnwrapSDKContext(ctx), token, destDomain)
}

// QuoteIgpFeeForToken returns the IGP fee required for a warp transfer of a specific token.
func (k Keeper) QuoteIgpFeeForToken(ctx sdk.Context, token warptypes.HypToken, destDomain uint32) (sdk.Coin, error) {
	gasLimit := math.ZeroInt()
	metadata := util.StandardHookMetadata{GasLimit: gasLimit}
	message := util.HyperlaneMessage{Destination: destDomain}

	quotedFee, err := k.hyperlaneKeeper.QuoteDispatch(ctx, token.OriginMailbox, util.NewZeroAddress(), metadata, message)
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("failed to quote dispatch: %w", err)
	}

	// Multi-denom IGP fees not supported
	if len(quotedFee) > 1 {
		return sdk.Coin{}, fmt.Errorf("multi-denom IGP fees not supported")
	}

	// Use the first coin from quoted fee, or zero if no fee required
	if len(quotedFee) > 0 {
		return quotedFee[0], nil
	}
	return sdk.NewCoin("utia", math.ZeroInt()), nil
}
