package testnode

import (
	"time"

	"github.com/celestiaorg/celestia-app/v4/app"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// wrapEndBlocker overrides the app's endblocker to set the timeout commit to a
// different value for testnode.
func wrapEndBlocker(app *app.App, timeoutCommit time.Duration) func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
	endBlocker := func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
		resp := app.EndBlocker(ctx, req)
		resp.Timeouts.TimeoutCommit = timeoutCommit
		return resp
	}

	return endBlocker
}
