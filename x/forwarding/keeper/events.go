package keeper

import (
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EmitTokenForwardedEvent emits an event for a single token forwarding result.
func EmitTokenForwardedEvent(
	ctx sdk.Context,
	forwardAddr, tokenID, denom, messageID string,
	amount math.Int,
) {
	if err := ctx.EventManager().EmitTypedEvent(&types.EventTokenForwarded{
		ForwardAddr: forwardAddr,
		TokenId:     tokenID,
		Denom:       denom,
		Amount:      amount,
		MessageId:   messageID,
	}); err != nil {
		ctx.Logger().Error("failed to emit EventTokenForwarded", "error", err)
	}
}
