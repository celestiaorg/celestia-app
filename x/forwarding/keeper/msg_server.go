package keeper

import (
	"context"
	"fmt"
	"strings"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k Keeper
}

func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// Forward forwards up to 20 tokens at forwardAddr to the committed destination.
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

	expectedAddr, err := types.DeriveForwardingAddress(msg.DestDomain, destRecipient.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to derive forwarding address: %w", err)
	}
	if !forwardAddr.Equals(sdk.AccAddress(expectedAddr)) {
		return nil, fmt.Errorf("%w: provided=%s derived=%s", types.ErrAddressMismatch, forwardAddr.String(), sdk.AccAddress(expectedAddr).String())
	}

	balances := m.k.bankKeeper.GetAllBalances(ctx, forwardAddr)
	if balances.IsZero() {
		return nil, types.ErrNoBalance
	}

	// Process up to MaxTokensPerForward tokens. User can call Forward again for remaining.
	if len(balances) > types.MaxTokensPerForward {
		balances = balances[:types.MaxTokensPerForward]
	}

	feeCollectorAddr := authtypes.NewModuleAddress(authtypes.FeeCollectorName)

	signerAddr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("invalid signer address: %w", err)
	}

	results := m.processTokens(ctx, forwardAddr, feeCollectorAddr, signerAddr, balances, msg, destRecipient)

	// If all tokens failed, return error (partial failure is OK, total failure is not)
	allFailed := true
	for _, r := range results {
		if r.Success {
			allFailed = false
			break
		}
	}
	if allFailed && len(results) > 0 {
		return nil, allTokensFailedError(results)
	}

	EmitForwardingCompleteEvent(ctx, msg.ForwardAddr, msg.DestDomain, msg.DestRecipient, results)
	return &types.MsgForwardResponse{Results: results}, nil
}

func (m msgServer) processTokens(
	ctx sdk.Context,
	forwardAddr, feeCollectorAddr, signerAddr sdk.AccAddress,
	balances sdk.Coins,
	msg *types.MsgForward,
	destRecipient util.HexAddress,
) []types.ForwardingResult {
	results := make([]types.ForwardingResult, 0, len(balances))

	for _, balance := range balances {
		result := m.forwardSingleToken(ctx, forwardAddr, feeCollectorAddr, signerAddr, balance, msg.DestDomain, destRecipient, msg.MaxIgpFee)
		results = append(results, result)

		EmitTokenForwardedEvent(ctx, msg.ForwardAddr, result)
	}

	return results
}

func (m msgServer) forwardSingleToken(
	ctx sdk.Context,
	forwardAddr, feeCollectorAddr, signerAddr sdk.AccAddress,
	balance sdk.Coin,
	destDomain uint32,
	destRecipient util.HexAddress,
	maxIgpFee sdk.Coin,
) types.ForwardingResult {
	hypToken, err := m.k.FindHypTokenByDenom(ctx, balance.Denom, destDomain)
	if err != nil {
		return types.NewFailureResult(balance.Denom, balance.Amount, fmt.Sprintf("token lookup failed: %s", err.Error()))
	}

	// For synthetic tokens, verify route exists (TIA route check is done in FindHypTokenByDenom)
	if balance.Denom != appconsts.BondDenom {
		hasRoute, err := m.k.HasEnrolledRouter(ctx, hypToken.Id, destDomain)
		if err != nil {
			return types.NewFailureResult(balance.Denom, balance.Amount, "error checking warp route: "+err.Error())
		}
		if !hasRoute {
			return types.NewFailureResult(balance.Denom, balance.Amount, types.ErrNoWarpRoute.Error())
		}
	}

	// Quote IGP fee for this token transfer
	quotedFee, err := m.k.QuoteIgpFeeForToken(ctx, hypToken, destDomain)
	if err != nil {
		return types.NewFailureResult(balance.Denom, balance.Amount, fmt.Sprintf("failed to quote IGP fee: %s", err.Error()))
	}

	// Verify relayer provided sufficient max_igp_fee
	// Only compare if quoted fee is positive and same denom
	if quotedFee.IsPositive() {
		if maxIgpFee.Denom != quotedFee.Denom {
			return types.NewFailureResult(balance.Denom, balance.Amount,
				fmt.Sprintf("max_igp_fee denom mismatch: provided %s, required %s", maxIgpFee.Denom, quotedFee.Denom))
		}
		if maxIgpFee.Amount.LT(quotedFee.Amount) {
			return types.NewFailureResult(balance.Denom, balance.Amount,
				fmt.Sprintf("%s: provided %s, required %s", types.ErrInsufficientIgpFee.Error(), maxIgpFee, quotedFee))
		}
	}

	// Capture IGP fee denom balance at forwardAddr before sending IGP fee
	// This allows us to track how much IGP fee was added and handle refunds
	igpBalanceBefore := m.k.bankKeeper.GetBalance(ctx, forwardAddr, quotedFee.Denom)

	// Send IGP fee from relayer (signer) directly to forwardAddr
	// If this fails, no state has changed - safe to return failure
	if quotedFee.IsPositive() {
		if err := m.k.bankKeeper.SendCoins(ctx, signerAddr, forwardAddr, sdk.NewCoins(quotedFee)); err != nil {
			return types.NewFailureResult(balance.Denom, balance.Amount,
				fmt.Sprintf("failed to collect IGP fee from relayer: %s", err.Error()))
		}
	}

	// Execute warp transfer with forwardAddr as sender
	// Warp has atomic semantics: on failure, tokens remain at forwardAddr
	messageId, err := m.k.ExecuteWarpTransfer(ctx, hypToken, forwardAddr.String(), destDomain, destRecipient, balance.Amount, quotedFee)
	if err != nil {
		// Warp failed - tokens remain at forwardAddr (warp is atomic)
		// Send IGP fee to fee collector so it becomes protocol revenue (distributed to stakers)
		// This incentivizes relayers to check route availability before submitting
		if quotedFee.IsPositive() {
			if consumeErr := m.k.bankKeeper.SendCoins(ctx, forwardAddr, feeCollectorAddr, sdk.NewCoins(quotedFee)); consumeErr != nil {
				ctx.Logger().Error("failed to send IGP fee to fee collector after warp failure",
					"denom", balance.Denom,
					"igp_fee", quotedFee.String(),
					"warp_error", err.Error(),
					"send_error", consumeErr.Error(),
				)
			}
		}
		return types.NewFailureResult(balance.Denom, balance.Amount, "warp transfer failed (tokens returned, IGP fee sent to fee collector): "+err.Error())
	}

	// Warp succeeded - refund any excess IGP fee to the relayer
	// Excess = (balance before + quoted fee) - balance after warp
	// The warp transfer consumes the actual IGP cost from forwardAddr
	if quotedFee.IsPositive() {
		igpBalanceAfter := m.k.bankKeeper.GetBalance(ctx, forwardAddr, quotedFee.Denom)
		// Expected: igpBalanceBefore (original) + quotedFee (sent) - actualIgpUsed = igpBalanceAfter
		// So excess = igpBalanceAfter - igpBalanceBefore (what remains beyond the original)
		excess := igpBalanceAfter.Amount.Sub(igpBalanceBefore.Amount)
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

	return types.NewSuccessResult(balance.Denom, balance.Amount, messageId.String())
}

func allTokensFailedError(results []types.ForwardingResult) error {
	failed := make([]string, 0, len(results))
	for _, result := range results {
		failed = append(failed, fmt.Sprintf("%s:%s (%s)", result.Denom, result.Amount.String(), result.GetError()))
	}

	return fmt.Errorf(
		"%w: all %d tokens failed to forward: %s",
		types.ErrAllTokensFailed,
		len(results),
		strings.Join(failed, "; "),
	)
}
