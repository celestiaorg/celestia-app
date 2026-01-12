package keeper

import (
	"context"
	"fmt"

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
		return nil, err
	}

	destRecipient, err := util.DecodeHexAddress(msg.DestRecipient)
	if err != nil {
		return nil, err
	}

	// destRecipient must be exactly 32 bytes
	if len(destRecipient.Bytes()) != 32 {
		return nil, types.ErrAddressMismatch
	}

	// 2. CRITICAL: Verify derived address matches
	expectedAddr := types.DeriveForwardingAddress(msg.DestDomain, destRecipient.Bytes())
	if !forwardAddr.Equals(expectedAddr) {
		return nil, types.ErrAddressMismatch
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
		// Use default params if not set - this is normal for fresh chains
		ctx.Logger().Debug("forwarding params not set, using defaults", "error", err.Error())
		params = types.DefaultParams()
	}

	// 7. Process each token
	var results []types.ForwardingResult

	for _, balance := range balances {
		result := m.forwardSingleToken(ctx, forwardAddr, moduleAddr, balance, msg.DestDomain, destRecipient, params)
		results = append(results, result)

		// Emit per-token event
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"token_forwarded",
				sdk.NewAttribute("forward_addr", msg.ForwardAddr),
				sdk.NewAttribute("denom", result.Denom),
				sdk.NewAttribute("amount", result.Amount.String()),
				sdk.NewAttribute("message_id", result.MessageId),
				sdk.NewAttribute("success", boolToString(result.Success)),
				sdk.NewAttribute("error", result.Error),
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
			"forwarding_complete",
			sdk.NewAttribute("forward_addr", msg.ForwardAddr),
			sdk.NewAttribute("dest_domain", uintToString(msg.DestDomain)),
			sdk.NewAttribute("dest_recipient", msg.DestRecipient),
			sdk.NewAttribute("tokens_forwarded", intToString(successCount)),
			sdk.NewAttribute("tokens_failed", intToString(failCount)),
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
	result := types.ForwardingResult{
		Denom:  balance.Denom,
		Amount: balance.Amount,
	}

	// PRE-CHECK 1: Find HypToken (before any transfer)
	hypToken, err := m.k.FindHypTokenByDenom(ctx, balance.Denom)
	if err != nil {
		result.Error = "unsupported token: " + balance.Denom
		return result // Token stays at forwardAddr ✓
	}

	// PRE-CHECK 2: Verify warp route exists (before any transfer)
	hasRoute, err := m.k.HasEnrolledRouter(ctx, hypToken.Id, destDomain)
	if err != nil || !hasRoute {
		result.Error = "no warp route to destination domain"
		return result // Token stays at forwardAddr ✓
	}

	// PRE-CHECK 3: Check minimum threshold
	if params.MinForwardAmount.IsPositive() && balance.Amount.LT(params.MinForwardAmount) {
		result.Error = "below minimum forward amount"
		return result // Token stays at forwardAddr ✓
	}

	// NOW safe to transfer - all pre-checks passed
	// Move tokens from forwardAddr to module account for warp transfer
	if err := m.k.bankKeeper.SendCoins(ctx, forwardAddr, moduleAddr, sdk.NewCoins(balance)); err != nil {
		result.Error = "failed to move tokens: " + err.Error()
		return result
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
			result.Error = fmt.Sprintf("warp transfer failed: %s; recovery failed: %s", err.Error(), recoveryErr.Error())
			return result
		}
		result.Error = "warp transfer failed (tokens returned to forward address): " + err.Error()
		return result
	}

	result.Success = true
	result.MessageId = messageId.String()
	return result
}

// Helper functions
func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func uintToString(u uint32) string {
	return fmt.Sprintf("%d", u)
}

func intToString(i int) string {
	return fmt.Sprintf("%d", i)
}
