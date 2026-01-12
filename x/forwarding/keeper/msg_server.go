package keeper

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// ExecuteForwarding handles MsgExecuteForwarding
// This is the core forwarding logic - forwards ALL tokens at forwardAddr to the committed destination.
//
// PARTIAL FAILURE BEHAVIOR (by design):
// Multi-token forwarding processes each token independently. If some tokens fail to forward
// (e.g., no warp route, below minimum threshold), the transaction still succeeds with mixed results.
// Failed tokens remain at forwardAddr and can be retried later. This design enables:
// - Permissionless retry: anyone can call ExecuteForwarding again for remaining tokens
// - Progressive forwarding: new warp routes can forward previously unsupported tokens
// - No stuck transactions: one bad token doesn't block others from forwarding
func (m msgServer) ExecuteForwarding(goCtx context.Context, msg *types.MsgExecuteForwarding) (*types.MsgExecuteForwardingResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Parse and validate inputs
	forwardAddr, err := sdk.AccAddressFromBech32(msg.ForwardAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid forward_addr %q: %w", msg.ForwardAddr, err)
	}

	destRecipient, err := util.DecodeHexAddress(msg.DestRecipient)
	if err != nil {
		return nil, fmt.Errorf("invalid dest_recipient hex: %w", err)
	}

	// destRecipient must be exactly 32 bytes
	if len(destRecipient.Bytes()) != 32 {
		return nil, fmt.Errorf("%w: dest_recipient must be 32 bytes, got %d", types.ErrAddressMismatch, len(destRecipient.Bytes()))
	}

	// 2. CRITICAL: Verify derived address matches
	expectedAddr := types.DeriveForwardingAddress(msg.DestDomain, destRecipient.Bytes())
	if !forwardAddr.Equals(expectedAddr) {
		return nil, fmt.Errorf("%w: provided=%s derived=%s", types.ErrAddressMismatch, forwardAddr.String(), expectedAddr.String())
	}

	// 3. Get ALL balances at forwardAddr
	balances := m.k.bankKeeper.GetAllBalances(ctx, forwardAddr)
	if balances.IsZero() {
		return nil, types.ErrNoBalance
	}

	// 4. Check token count limit to prevent gas exhaustion
	if len(balances) > types.MaxTokensPerForward {
		return nil, types.ErrTooManyTokens
	}

	// 5. Get module address for token custody during warp transfer
	moduleAddr := m.k.accountKeeper.GetModuleAddress(types.ModuleName)

	// 6. Get params for minimum threshold check
	params, err := m.k.GetParams(ctx)
	if err != nil {
		// Distinguish between "not found" (acceptable) vs actual read errors
		if errors.Is(err, collections.ErrNotFound) {
			ctx.Logger().Info("forwarding params not configured, using defaults")
			params = types.DefaultParams()
		} else {
			// Actual storage error - do not silently continue
			return nil, fmt.Errorf("failed to read module params: %w", err)
		}
	}

	// 7. Process each token
	var results []types.ForwardingResult

	for _, balance := range balances {
		result := m.forwardSingleToken(ctx, forwardAddr, moduleAddr, balance, msg.DestDomain, destRecipient, params)
		results = append(results, result)

		// Emit per-token event
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeTokenForwarded,
				sdk.NewAttribute(types.AttributeKeyForwardAddr, msg.ForwardAddr),
				sdk.NewAttribute(types.AttributeKeyDenom, result.Denom),
				sdk.NewAttribute(types.AttributeKeyAmount, result.Amount.String()),
				sdk.NewAttribute(types.AttributeKeyMessageId, result.MessageId),
				sdk.NewAttribute(types.AttributeKeySuccess, strconv.FormatBool(result.Success)),
				sdk.NewAttribute(types.AttributeKeyError, result.Error),
			),
		)
	}

	// 8. Emit summary event
	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failCount++
		}
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeForwardingComplete,
			sdk.NewAttribute(types.AttributeKeyForwardAddr, msg.ForwardAddr),
			sdk.NewAttribute(types.AttributeKeyDestDomain, strconv.FormatUint(uint64(msg.DestDomain), 10)),
			sdk.NewAttribute(types.AttributeKeyDestRecipient, msg.DestRecipient),
			sdk.NewAttribute(types.AttributeKeyTokensForwarded, strconv.Itoa(successCount)),
			sdk.NewAttribute(types.AttributeKeyTokensFailed, strconv.Itoa(failCount)),
		),
	)

	return &types.MsgExecuteForwardingResponse{Results: results}, nil
}

// forwardSingleToken handles forwarding a single token
// Uses PRE-CHECK pattern: verify warp route BEFORE SendCoins to keep failed tokens at forwardAddr
func (m msgServer) forwardSingleToken(
	ctx sdk.Context,
	forwardAddr sdk.AccAddress,
	moduleAddr sdk.AccAddress,
	balance sdk.Coin,
	destDomain uint32,
	destRecipient util.HexAddress,
	params types.Params,
) types.ForwardingResult {
	// PRE-CHECK 1: Find HypToken (before any transfer)
	hypToken, err := m.k.FindHypTokenByDenom(ctx, balance.Denom)
	if err != nil {
		return types.NewFailureResult(balance.Denom, balance.Amount, "unsupported token: "+balance.Denom)
	}

	// PRE-CHECK 2: Verify warp route exists (before any transfer)
	hasRoute, err := m.k.HasEnrolledRouter(ctx, hypToken.Id, destDomain)
	if err != nil {
		return types.NewFailureResult(balance.Denom, balance.Amount, fmt.Sprintf("error checking warp route: %s", err.Error()))
	}
	if !hasRoute {
		return types.NewFailureResult(balance.Denom, balance.Amount, "no warp route to destination domain")
	}

	// PRE-CHECK 3: Check minimum threshold
	if params.MinForwardAmount.IsPositive() && balance.Amount.LT(params.MinForwardAmount) {
		return types.NewFailureResult(balance.Denom, balance.Amount, "below minimum forward amount")
	}

	// NOW safe to transfer - all pre-checks passed
	// Move tokens from forwardAddr to module account for warp transfer
	if err := m.k.bankKeeper.SendCoins(ctx, forwardAddr, moduleAddr, sdk.NewCoins(balance)); err != nil {
		return types.NewFailureResult(balance.Denom, balance.Amount, "failed to move tokens: "+err.Error())
	}

	// Execute warp transfer
	messageId, err := m.k.ExecuteWarpTransfer(
		ctx,
		hypToken,
		moduleAddr.String(),
		destDomain,
		destRecipient,
		balance.Amount,
	)
	if err != nil {
		// Rare edge case: warp transfer failed after pre-checks passed.
		// Recovery: return tokens to forwardAddr so user can retry.
		if recoveryErr := m.k.bankKeeper.SendCoins(ctx, moduleAddr, forwardAddr, sdk.NewCoins(balance)); recoveryErr != nil {
			// If recovery also fails, log both errors
			return types.NewFailureResult(balance.Denom, balance.Amount, fmt.Sprintf("warp transfer failed: %s; recovery failed: %s", err.Error(), recoveryErr.Error()))
		}
		return types.NewFailureResult(balance.Denom, balance.Amount, "warp transfer failed (tokens returned to forward address): "+err.Error())
	}

	return types.NewSuccessResult(balance.Denom, balance.Amount, messageId.String())
}

