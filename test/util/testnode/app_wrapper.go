package testnode

import (
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

func GetTimeoutCommit(v uint64) time.Duration {
	switch v {
	case v1.Version:
		return time.Millisecond * 500
	case v2.Version:
		return time.Millisecond * 500
	default:
		return time.Millisecond * 100
	}
}

func WrapEndBlocker(app *app.App, timeoutCommit time.Duration) func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
	currentAppVersion := app.AppVersion()
	endBlocker := func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
		resp := app.EndBlocker(ctx, req)
		resp.Timeouts.TimeoutCommit = GetTimeoutCommit(currentAppVersion)
		return resp
	}

	return endBlocker
}
