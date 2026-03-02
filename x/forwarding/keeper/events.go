package keeper

import (
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
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
