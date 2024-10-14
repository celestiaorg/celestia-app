package testnode

import (
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

func WrapEndBlocker(app *app.App, timeoutCommit time.Duration) func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
	endBlocker := func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
		resp := app.EndBlocker(ctx, req)
		resp.Timeouts.TimeoutCommit = timeoutCommit
		return resp
	}

	return endBlocker
}
