package keeper

import (
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EmitTokenForwardedEvent emits an event for a single token forwarding result.
func EmitTokenForwardedEvent(ctx sdk.Context, forwardAddr string, result types.ForwardingResult) {
	if err := ctx.EventManager().EmitTypedEvent(&types.EventTokenForwarded{
		ForwardAddr: forwardAddr,
		Denom:       result.Denom,
		Amount:      result.Amount,
		MessageId:   result.MessageId,
		Success:     result.Success,
		Error:       result.Error,
	}); err != nil {
		ctx.Logger().Error("failed to emit EventTokenForwarded", "error", err)
	}
}

// EmitForwardingCompleteEvent emits a summary event for the entire forwarding operation.
func EmitForwardingCompleteEvent(ctx sdk.Context, forwardAddr string, destDomain uint32, destRecipient string, results []types.ForwardingResult) {
	var successCount uint32
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}
	failCount := uint32(len(results)) - successCount

	if err := ctx.EventManager().EmitTypedEvent(&types.EventForwardingComplete{
		ForwardAddr:     forwardAddr,
		DestDomain:      destDomain,
		DestRecipient:   destRecipient,
		TokensForwarded: successCount,
		TokensFailed:    failCount,
	}); err != nil {
		ctx.Logger().Error("failed to emit EventForwardingComplete", "error", err)
	}
}

// EmitTokensStuckEvent emits an event when tokens become stuck in the module account.
func EmitTokensStuckEvent(ctx sdk.Context, denom string, amount math.Int, moduleAddr, forwardAddr, errorMsg string) {
	if err := ctx.EventManager().EmitTypedEvent(&types.EventTokensStuck{
		Denom:       denom,
		Amount:      amount,
		ModuleAddr:  moduleAddr,
		ForwardAddr: forwardAddr,
		Error:       errorMsg,
	}); err != nil {
		ctx.Logger().Error("failed to emit EventTokensStuck", "error", err)
	}
}
