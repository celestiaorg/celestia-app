package testnode

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

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
// called during cleanup to teardown the node, core client, along with canceling
// the internal context.Context in the returned Context.
func StartNode(tmNode *node.Node, cctx Context) (Context, func() error, error) {
	if err := tmNode.Start(); err != nil {
		return cctx, func() error { return nil }, err
	}

	coreClient := local.New(tmNode)

	cctx.Context = cctx.WithClient(coreClient)
	goCtx, cancel := context.WithCancel(context.Background())
	cctx.rootCtx = goCtx
	cleanup := func() error {
		cancel()
		err := tmNode.Stop()
		if err != nil {
			return err
		}
		tmNode.Wait()
		return removeDir(path.Join([]string{cctx.HomeDir, "config"}...))
	}

	return cctx, cleanup, nil
}

// StartGRPCServer starts the grpc server using the provided application and
// config. A grpc client connection to that server is also added to the client
// context. The returned function should be used to shutdown the server.
func StartGRPCServer(app srvtypes.Application, appCfg *srvconfig.Config, cctx Context) (Context, func() error, error) {
	emptycleanup := func() error { return nil }
	// Add the tx service in the gRPC router.
	app.RegisterTxService(cctx.Context)

	// Add the tendermint queries service in the gRPC router.
	app.RegisterTendermintService(cctx.Context)

	// Add the node service queries to the grpc router.
	if a, ok := app.(srvtypes.ApplicationQueryService); ok {
		a.RegisterNodeService(cctx.Context)
	}

	grpcSrv, err := srvgrpc.StartGRPCServer(cctx.Context, app, appCfg.GRPC)
	if err != nil {
		return Context{}, emptycleanup, err
	}

	nodeGRPCAddr := strings.Replace(appCfg.GRPC.Address, "0.0.0.0", "localhost", 1)
	conn, err := grpc.NewClient(nodeGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return Context{}, emptycleanup, err
	}

	cctx.Context = cctx.WithGRPCClient(conn)

	return cctx, func() error {
		grpcSrv.Stop()
		return nil
	}, nil
}

// DefaultAppConfig wraps the default config described in the server
func DefaultAppConfig() *srvconfig.Config {
	appCfg := srvconfig.DefaultConfig()
	appCfg.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", GetFreePort())
	appCfg.API.Address = fmt.Sprintf("tcp://127.0.0.1:%d", GetFreePort())
	return appCfg
}

// removeDir removes the directory `rootDir`.
// The main use of this is to reduce the flakiness of the CI when it's unable to delete
// the config folder of the tendermint node.
// This will manually go over the files contained inside the provided `rootDir`
// and delete them one by one.
func removeDir(rootDir string) error {
	dir, err := os.ReadDir(rootDir)
	if err != nil {
		return err
	}
	for _, d := range dir {
		err := os.RemoveAll(path.Join([]string{rootDir, d.Name()}...))
		if err != nil {
			return err
		}
	}
	return os.RemoveAll(rootDir)
}
