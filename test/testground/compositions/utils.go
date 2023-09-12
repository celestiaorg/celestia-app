package compositions

import (
	"context"
	"time"

	"github.com/testground/sdk-go/network"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

func InitTest(runenv *runtime.RunEnv, initCtx *run.InitContext) (*run.InitContext, context.Context, context.CancelFunc, error) {
	syncclient := initCtx.SyncClient
	netclient := network.NewClient(syncclient, runenv)
	timeout, err := time.ParseDuration(runenv.TestInstanceParams["timeout"])
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	netclient.MustWaitNetworkInitialized(ctx)
	initCtx.NetClient = netclient
	return initCtx, ctx, cancel, nil
}
