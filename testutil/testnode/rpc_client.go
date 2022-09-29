package testnode

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	srvgrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/rpc/client/local"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// StartNode starts the tendermint node along with a local core rpc client. The
// rpc is returned via the client.Context. The function returned should be
// called during cleanup to teardown the node and core client.
func StartNode(tmNode *node.Node, cctx client.Context) (client.Context, func(), error) {
	if err := tmNode.Start(); err != nil {
		return cctx, func() {}, err
	}

	coreClient := local.New(tmNode)

	cctx = cctx.WithClient(coreClient)
	cleanup := func() {
		_ = tmNode.Stop()
		_ = coreClient.Stop()
	}

	return cctx, cleanup, nil
}

// StartGRPCServer starts the grpc server using the provided application and
// config. A grpc client connection to that server is also added to the client
// context. The returned function should be used to shutdown the server.
func StartGRPCServer(app srvtypes.Application, appCfg srvconfig.Config, cctx client.Context) (client.Context, func(), error) {
	// Add the tx service in the gRPC router.
	app.RegisterTxService(cctx)

	// Add the tendermint queries service in the gRPC router.
	app.RegisterTendermintService(cctx)

	grpcSrv, err := srvgrpc.StartGRPCServer(cctx, app, appCfg.GRPC)
	if err != nil {
		return cctx, func() {}, err
	}

	nodeGRPCAddr := strings.Replace(appCfg.GRPC.Address, "0.0.0.0", "localhost", 1)
	conn, err := grpc.Dial(nodeGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return cctx, func() {}, err
	}

	return cctx.WithGRPCClient(conn), grpcSrv.Stop, nil
}

// DefaultAppConfig wraps the default config described in the ser
func DefaultAppConfig() *srvconfig.Config {
	return srvconfig.DefaultConfig()
}

// LatestHeight returns the latest height of the network or an error if the
// query fails.
func LatestHeight(cctx client.Context) (int64, error) {
	status, err := cctx.Client.Status(context.Background())
	if err != nil {
		return 0, err
	}

	return status.SyncInfo.LatestBlockHeight, nil
}

// WaitForHeightWithTimeout is the same as WaitForHeight except the caller can
// provide a custom timeout.
func WaitForHeightWithTimeout(cctx client.Context, h int64, t time.Duration) (int64, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	timeout := time.NewTimer(t)
	defer timeout.Stop()

	var latestHeight int64
	for {
		select {
		case <-timeout.C:
			return latestHeight, errors.New("timeout exceeded waiting for block")
		case <-ticker.C:
			latestHeight, err := LatestHeight(cctx)
			if err != nil {
				return 0, err
			}
			if latestHeight >= h {
				return latestHeight, nil
			}
		}
	}
}

// WaitForHeight performs a blocking check where it waits for a block to be
// committed after a given block. If that height is not reached within a timeout,
// an error is returned. Regardless, the latest height queried is returned.
func WaitForHeight(cctx client.Context, h int64) (int64, error) {
	return WaitForHeightWithTimeout(cctx, h, 10*time.Second)
}

// WaitForNextBlock waits for the next block to be committed, returning an error
// upon failure.
func WaitForNextBlock(cctx client.Context) error {
	lastBlock, err := LatestHeight(cctx)
	if err != nil {
		return err
	}

	_, err = WaitForHeight(cctx, lastBlock+1)
	if err != nil {
		return err
	}

	return err
}
