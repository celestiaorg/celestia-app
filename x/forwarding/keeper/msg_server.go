package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k Keeper
}

func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// Forward forwards the token bound to forwardAddr to the committed destination.
func (m msgServer) Forward(goCtx context.Context, msg *types.MsgForward) (*types.MsgForwardResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	forwardAddr, err := sdk.AccAddressFromBech32(msg.ForwardAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid forward_addr %q: %w", msg.ForwardAddr, err)
	}

	destRecipient, err := util.DecodeHexAddress(msg.DestRecipient)
	if err != nil {
		return nil, fmt.Errorf("invalid dest_recipient hex: %w", err)
	}

	tokenID, err := util.DecodeHexAddress(msg.TokenId)
	if err != nil {
		return nil, fmt.Errorf("invalid token_id hex: %w", err)
	}

	expectedAddr, err := types.DeriveForwardingAddress(msg.DestDomain, destRecipient.Bytes(), tokenID.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to derive forwarding address: %w", err)
	}
	if !forwardAddr.Equals(sdk.AccAddress(expectedAddr)) {
		return nil, fmt.Errorf("%w: provided=%s derived=%s", types.ErrAddressMismatch, forwardAddr.String(), sdk.AccAddress(expectedAddr).String())
	}

	signerAddr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("invalid signer address: %w", err)
	}

	hypToken, err := m.k.warpKeeper.GetHypToken(ctx, tokenID.GetInternalId())
	if err != nil {
		return nil, fmt.Errorf("token %s not found: %w", msg.TokenId, err)
	}

	denom, err := m.k.BankDenomForToken(hypToken)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve token denom: %w", err)
	}

	balance := m.k.bankKeeper.GetBalance(ctx, forwardAddr, denom)
	if !balance.IsPositive() {
		return nil, types.ErrNoBalance
	}

	messageID, err := m.forwardToken(ctx, forwardAddr, signerAddr, hypToken, balance, msg.DestDomain, destRecipient, msg.MaxIgpFee)
	if err != nil {
		return nil, fmt.Errorf("%w: %s:%s (%s)", types.ErrAllTokensFailed, balance.Denom, balance.Amount.String(), err)
	}

	EmitTokenForwardedEvent(ctx, msg.ForwardAddr, msg.TokenId, balance.Denom, messageID.String(), balance.Amount)

	return &types.MsgForwardResponse{
		Denom:     balance.Denom,
		Amount:    balance.Amount,
		MessageId: messageID.String(),
	}, nil
}

func (m msgServer) forwardToken(
	ctx sdk.Context,
	forwardAddr, signerAddr sdk.AccAddress,
	hypToken warptypes.HypToken,
	balance sdk.Coin,
	destDomain uint32,
	destRecipient util.HexAddress,
	maxIgpFee sdk.Coin,
) (util.HexAddress, error) {
	hasRoute, err := m.k.HasEnrolledRouter(ctx, hypToken.Id, destDomain)
	if err != nil {
		return util.HexAddress{}, fmt.Errorf("error checking warp route: %w", err)
	}
	if !hasRoute {
		return util.HexAddress{}, types.ErrNoWarpRoute
	}

	// Quote IGP fee for this token transfer
	quotedFee, err := m.k.QuoteIgpFeeForToken(ctx, hypToken, destDomain)
	if err != nil {
		return util.HexAddress{}, fmt.Errorf("failed to quote IGP fee: %w", err)
	}

	// Verify relayer provided sufficient max_igp_fee
	// Only compare if quoted fee is positive and same denom
	if quotedFee.IsPositive() {
		if maxIgpFee.Denom != quotedFee.Denom {
			return util.HexAddress{}, fmt.Errorf("max_igp_fee denom mismatch: provided %s, required %s", maxIgpFee.Denom, quotedFee.Denom)
		}
		if maxIgpFee.Amount.LT(quotedFee.Amount) {
			return util.HexAddress{}, fmt.Errorf("%w: provided %s, required %s", types.ErrInsufficientIgpFee, maxIgpFee, quotedFee)
		}
	}

	// Capture IGP fee denom balance at forwardAddr before sending IGP fee
	// This allows us to track how much IGP fee was added and handle refunds
	feeDenomBalance := m.k.bankKeeper.GetBalance(ctx, forwardAddr, quotedFee.Denom)

	// Send IGP fee from relayer (signer) directly to forwardAddr
	// If this fails, no state has changed - safe to return failure
	if quotedFee.IsPositive() {
		if err := m.k.bankKeeper.SendCoins(ctx, signerAddr, forwardAddr, sdk.NewCoins(quotedFee)); err != nil {
			return util.HexAddress{}, fmt.Errorf("failed to collect IGP fee from relayer: %w", err)
		}
	}

	// Execute warp transfer with forwardAddr as sender. The caller wraps this
	// attempt in a CacheContext so failures discard all token-level state changes.
	messageId, err := m.k.ExecuteWarpTransfer(ctx, hypToken, forwardAddr.String(), destDomain, destRecipient, balance.Amount, quotedFee)
	if err != nil {
		return util.HexAddress{}, fmt.Errorf("warp transfer failed: %w", err)
	}

	// Warp succeeded - refund any excess IGP fee to the relayer.
	if quotedFee.IsPositive() {
		feeDenomBalanceAfter := m.k.bankKeeper.GetBalance(ctx, forwardAddr, quotedFee.Denom)
		excess := calculateExcessIGPFee(feeDenomBalance, feeDenomBalanceAfter, quotedFee, balance)

		if excess.IsPositive() {
			excessCoin := sdk.NewCoin(quotedFee.Denom, excess)
			if refundErr := m.k.bankKeeper.SendCoins(ctx, forwardAddr, signerAddr, sdk.NewCoins(excessCoin)); refundErr != nil {
				ctx.Logger().Error("failed to refund excess IGP fee to relayer",
					"denom", balance.Denom,
					"excess", excessCoin.String(),
					"refund_error", refundErr.Error(),
				)
			}
		}
	}

	return messageId, nil
}

func calculateExcessIGPFee(before, after, quotedFee, forwardedBalance sdk.Coin) math.Int {
	igpUsed := before.Amount.Add(quotedFee.Amount).Sub(after.Amount)
	if forwardedBalance.Denom == quotedFee.Denom {
		igpUsed = igpUsed.Sub(forwardedBalance.Amount)
	}

	return quotedFee.Amount.Sub(igpUsed)
}
