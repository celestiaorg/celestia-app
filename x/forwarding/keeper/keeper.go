// Package keeper implements the forwarding module keeper, which manages
// automatic cross-chain token forwarding via Hyperlane warp routes.
package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/x/forwarding/types"
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

func (k Keeper) HasEnrolledRouter(ctx context.Context, tokenId util.HexAddress, destDomain uint32) (bool, error) {
	return k.warpKeeper.HasEnrolledRouter(ctx, tokenId.GetInternalId(), destDomain)
}

// GetEnrolledRouter returns the RemoteRouter for a token and destination domain.
func (k Keeper) GetEnrolledRouter(ctx context.Context, tokenId util.HexAddress, destDomain uint32) (warptypes.RemoteRouter, error) {
	return k.warpKeeper.GetEnrolledRouter(ctx, tokenId.GetInternalId(), destDomain)
}

// BankDenomForToken returns the bank denom used to represent the given Hyperlane token on Celestia.
func (k Keeper) BankDenomForToken(token warptypes.HypToken) (string, error) {
	switch token.TokenType {
	case warptypes.HYP_TOKEN_TYPE_COLLATERAL:
		return token.OriginDenom, nil
	case warptypes.HYP_TOKEN_TYPE_SYNTHETIC:
		return "hyperlane/" + token.Id.String(), nil
	default:
		return "", types.ErrUnsupportedToken
	}
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
