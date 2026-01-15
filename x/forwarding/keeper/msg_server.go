package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k Keeper
}

func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// ExecuteForwarding forwards ALL tokens at forwardAddr to the committed destination.
// Partial failures are by design: failed tokens remain for retry while others proceed.
func (m msgServer) ExecuteForwarding(goCtx context.Context, msg *types.MsgExecuteForwarding) (*types.MsgExecuteForwardingResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	forwardAddr, err := sdk.AccAddressFromBech32(msg.ForwardAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid forward_addr %q: %w", msg.ForwardAddr, err)
	}

	destRecipient, err := util.DecodeHexAddress(msg.DestRecipient)
	if err != nil {
		return nil, fmt.Errorf("invalid dest_recipient hex: %w", err)
	}

	if len(destRecipient.Bytes()) != types.RecipientLength {
		return nil, fmt.Errorf("%w: dest_recipient must be %d bytes, got %d", types.ErrAddressMismatch, types.RecipientLength, len(destRecipient.Bytes()))
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

	if len(balances) > types.MaxTokensPerForward {
		return nil, types.ErrTooManyTokens
	}

	moduleAddr := m.k.accountKeeper.GetModuleAddress(types.ModuleName)

	params, err := m.k.GetParams(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			ctx.Logger().Warn("forwarding module params not configured, using defaults - TIA forwarding will fail until params are set")
			params = types.DefaultParams()
		} else {
			return nil, fmt.Errorf("failed to read module params: %w", err)
		}
	}

	results := m.processTokens(ctx, forwardAddr, moduleAddr, balances, msg, destRecipient, params)
	m.emitSummaryEvent(ctx, msg, results)

	return &types.MsgExecuteForwardingResponse{Results: results}, nil
}

func (m msgServer) processTokens(
	ctx sdk.Context,
	forwardAddr, moduleAddr sdk.AccAddress,
	balances sdk.Coins,
	msg *types.MsgExecuteForwarding,
	destRecipient util.HexAddress,
	params types.Params,
) []types.ForwardingResult {
	results := make([]types.ForwardingResult, 0, len(balances))

	for _, balance := range balances {
		result := m.forwardSingleToken(ctx, forwardAddr, moduleAddr, balance, msg.DestDomain, destRecipient, params)
		results = append(results, result)

		if err := ctx.EventManager().EmitTypedEvent(&types.EventTokenForwarded{
			ForwardAddr: msg.ForwardAddr,
			Denom:       result.Denom,
			Amount:      result.Amount,
			MessageId:   result.MessageId,
			Success:     result.Success,
			Error:       result.Error,
		}); err != nil {
			ctx.Logger().Error("failed to emit EventTokenForwarded", "error", err)
		}
	}

	return results
}

func (m msgServer) emitSummaryEvent(ctx sdk.Context, msg *types.MsgExecuteForwarding, results []types.ForwardingResult) {
	var successCount uint32
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}
	failCount := uint32(len(results)) - successCount

	if err := ctx.EventManager().EmitTypedEvent(&types.EventForwardingComplete{
		ForwardAddr:     msg.ForwardAddr,
		DestDomain:      msg.DestDomain,
		DestRecipient:   msg.DestRecipient,
		TokensForwarded: successCount,
		TokensFailed:    failCount,
	}); err != nil {
		ctx.Logger().Error("failed to emit EventForwardingComplete", "error", err)
	}
}

func (m msgServer) forwardSingleToken(
	ctx sdk.Context,
	forwardAddr, moduleAddr sdk.AccAddress,
	balance sdk.Coin,
	destDomain uint32,
	destRecipient util.HexAddress,
	params types.Params,
) types.ForwardingResult {
	hypToken, err := m.k.FindHypTokenByDenom(ctx, balance.Denom, destDomain)
	if err != nil {
		return types.NewFailureResult(balance.Denom, balance.Amount, fmt.Sprintf("token lookup failed: %s", err.Error()))
	}

	// For synthetic tokens, verify route exists (TIA route check is done in FindHypTokenByDenom)
	if balance.Denom != "utia" {
		hasRoute, err := m.k.HasEnrolledRouter(ctx, hypToken.Id, destDomain)
		if err != nil {
			return types.NewFailureResult(balance.Denom, balance.Amount, "error checking warp route: "+err.Error())
		}
		if !hasRoute {
			return types.NewFailureResult(balance.Denom, balance.Amount, "no warp route to destination domain")
		}
	}

	if params.MinForwardAmount.IsPositive() && balance.Amount.LT(params.MinForwardAmount) {
		return types.NewFailureResult(balance.Denom, balance.Amount, "below minimum forward amount")
	}

	if err := m.k.bankKeeper.SendCoins(ctx, forwardAddr, moduleAddr, sdk.NewCoins(balance)); err != nil {
		return types.NewFailureResult(balance.Denom, balance.Amount, "failed to move tokens: "+err.Error())
	}

	messageId, err := m.k.ExecuteWarpTransfer(ctx, hypToken, moduleAddr.String(), destDomain, destRecipient, balance.Amount)
	if err != nil {
		if recoveryErr := m.k.bankKeeper.SendCoins(ctx, moduleAddr, forwardAddr, sdk.NewCoins(balance)); recoveryErr != nil {
			ctx.Logger().Error("CRITICAL: tokens stuck in module account after failed recovery",
				"denom", balance.Denom,
				"amount", balance.Amount.String(),
				"module_addr", moduleAddr.String(),
				"forward_addr", forwardAddr.String(),
				"warp_error", err.Error(),
				"recovery_error", recoveryErr.Error(),
			)
			if emitErr := ctx.EventManager().EmitTypedEvent(&types.EventTokensStuck{
				Denom:       balance.Denom,
				Amount:      balance.Amount,
				ModuleAddr:  moduleAddr.String(),
				ForwardAddr: forwardAddr.String(),
				Error:       fmt.Sprintf("warp: %s; recovery: %s", err.Error(), recoveryErr.Error()),
			}); emitErr != nil {
				ctx.Logger().Error("failed to emit EventTokensStuck", "error", emitErr)
			}
			return types.NewFailureResult(balance.Denom, balance.Amount, fmt.Sprintf("CRITICAL: warp failed and recovery failed - tokens stuck in module account: warp=%s recovery=%s", err.Error(), recoveryErr.Error()))
		}
		return types.NewFailureResult(balance.Denom, balance.Amount, "warp transfer failed (tokens returned): "+err.Error())
	}

	return types.NewSuccessResult(balance.Denom, balance.Amount, messageId.String())
}

// UpdateForwardingParams updates the module parameters.
func (m msgServer) UpdateForwardingParams(goCtx context.Context, msg *types.MsgUpdateForwardingParams) (*types.MsgUpdateForwardingParamsResponse, error) {
	if m.k.authority != msg.Authority {
		return nil, fmt.Errorf("invalid authority: expected %s, got %s", m.k.authority, msg.Authority)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := m.k.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateForwardingParamsResponse{}, nil
}
