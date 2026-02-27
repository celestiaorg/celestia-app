// Package keeper implements the forwarding module keeper, which manages
// automatic cross-chain token forwarding via Hyperlane warp routes.
package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper manages forwarding module state and coordinates with Hyperlane warp for cross-chain transfers.
type Keeper struct {
	bankKeeper      types.BankKeeper
	warpKeeper      types.WarpKeeper
	hyperlaneKeeper types.HyperlaneKeeper
}

func NewKeeper(
	bankKeeper types.BankKeeper,
	warpKeeper types.WarpKeeper,
	hyperlaneKeeper types.HyperlaneKeeper,
) Keeper {
	if bankKeeper == nil {
		panic("bankKeeper cannot be nil")
	}
	if warpKeeper == nil {
		panic("warpKeeper cannot be nil")
	}
	if hyperlaneKeeper == nil {
		panic("hyperlaneKeeper cannot be nil")
	}

	return Keeper{
		bankKeeper:      bankKeeper,
		warpKeeper:      warpKeeper,
		hyperlaneKeeper: hyperlaneKeeper,
	}
}

// FindHypTokenByDenom finds the HypToken for a given bank denom and destination domain.
// For TIA, it searches warp tokens with OriginDenom=appconsts.BondDenom that have a route to destDomain.
// For synthetic tokens (hyperlane/...), it looks up the token by ID.
func (k Keeper) FindHypTokenByDenom(ctx context.Context, denom string, destDomain uint32) (warptypes.HypToken, error) {
	switch {
	case denom == appconsts.BondDenom:
		return k.findTIACollateralTokenForDomain(ctx, destDomain)
	case strings.HasPrefix(denom, "hyperlane/"):
		return k.getTokenById(ctx, strings.TrimPrefix(denom, "hyperlane/"))
	default:
		return warptypes.HypToken{}, types.ErrUnsupportedToken
	}
}

// findTokenWithRoute iterates warp tokens and returns the first one matching
// the filter that has an enrolled router for destDomain.
//
// Note: This is O(n) where n = number of warp tokens. Optimization would require an index
// in the warp keeper mapping (denom, destDomain) -> tokenId, which is outside this module's scope.
func (k Keeper) findTokenWithRoute(ctx context.Context, destDomain uint32, filter func(warptypes.HypToken) bool) (warptypes.HypToken, bool, error) {
	tokens, err := k.warpKeeper.GetAllHypTokens(ctx)
	if err != nil {
		return warptypes.HypToken{}, false, fmt.Errorf("failed to get warp tokens: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, token := range tokens {
		if filter != nil && !filter(token) {
			continue
		}
		hasRoute, routeErr := k.HasEnrolledRouter(ctx, token.Id, destDomain)
		if routeErr != nil {
			sdkCtx.Logger().Warn("failed to check enrolled router", "tokenId", token.Id.String(), "destDomain", destDomain, "error", routeErr)
			continue
		}
		if hasRoute {
			return token, true, nil
		}
	}
	return warptypes.HypToken{}, false, nil
}

// findTIACollateralTokenForDomain finds the TIA collateral token with a route to the destination domain.
func (k Keeper) findTIACollateralTokenForDomain(ctx context.Context, destDomain uint32) (warptypes.HypToken, error) {
	filter := func(token warptypes.HypToken) bool {
		return token.OriginDenom == appconsts.BondDenom && token.TokenType == warptypes.HYP_TOKEN_TYPE_COLLATERAL
	}
	token, found, err := k.findTokenWithRoute(ctx, destDomain, filter)
	if err != nil {
		return warptypes.HypToken{}, err
	}
	if !found {
		return warptypes.HypToken{}, fmt.Errorf("%w: no TIA collateral route to domain %d", types.ErrNoWarpRoute, destDomain)
	}
	return token, nil
}

func (k Keeper) getTokenById(ctx context.Context, tokenIdHex string) (warptypes.HypToken, error) {
	tokenId, err := util.DecodeHexAddress(tokenIdHex)
	if err != nil {
		return warptypes.HypToken{}, fmt.Errorf("%w: invalid token ID %q", types.ErrUnsupportedToken, tokenIdHex)
	}
	token, err := k.warpKeeper.GetHypToken(ctx, tokenId.GetInternalId())
	if err != nil {
		return warptypes.HypToken{}, fmt.Errorf("token %s not found: %w", tokenIdHex, err)
	}
	return token, nil
}

func (k Keeper) HasEnrolledRouter(ctx context.Context, tokenId util.HexAddress, destDomain uint32) (bool, error) {
	return k.warpKeeper.HasEnrolledRouter(ctx, tokenId.GetInternalId(), destDomain)
}

// GetEnrolledRouter returns the RemoteRouter for a token and destination domain.
func (k Keeper) GetEnrolledRouter(ctx context.Context, tokenId util.HexAddress, destDomain uint32) (warptypes.RemoteRouter, error) {
	return k.warpKeeper.GetEnrolledRouter(ctx, tokenId.GetInternalId(), destDomain)
}

// HasAnyRouteToDestination returns true if any warp token has an enrolled router
// for the destination domain. This validates that forwarding is possible before
// deriving an address, regardless of which token type will be forwarded.
func (k Keeper) HasAnyRouteToDestination(ctx context.Context, destDomain uint32) (bool, error) {
	_, found, err := k.findTokenWithRoute(ctx, destDomain, nil)
	return found, err
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
	router, err := k.GetEnrolledRouter(ctx, token.Id, destDomain)
	if err != nil {
		return util.HexAddress{}, fmt.Errorf("no router for domain %d: %w", destDomain, err)
	}
	gasLimit := router.Gas

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
	router, err := k.GetEnrolledRouter(ctx, token.Id, destDomain)
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("no router for domain %d: %w", destDomain, err)
	}
	gasLimit := router.Gas

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
	return sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()), nil
}
