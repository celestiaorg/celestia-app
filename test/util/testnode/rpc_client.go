package testnode

import (
	"path"
	"strings"
	"time"

	"cosmossdk.io/log"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/rpc/client/local"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server/api"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	srvgrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// noOpCleanup is a function that conforms to the cleanup function signature and
// performs no operation.
var noOpCleanup = func() error { return nil }

// StartNode starts the Comet node along with a local core RPC client. The
// RPC is returned via the client.Context. The function returned should be
// called during cleanup to teardown the node, core client, along with canceling
// the internal context.Context in the returned Context.
func StartNode(cometNode *node.Node, cctx Context) (Context, func() error, error) {
	if err := cometNode.Start(); err != nil {
		return cctx, noOpCleanup, err
	}
	client := local.New(cometNode)
	cctx.Context = cctx.WithClient(client)
	cleanup := func() error {
		err := cometNode.Stop()
		if err != nil {
			return err
		}
		cometNode.Wait()
		if err = removeDir(path.Join([]string{cctx.HomeDir, "config"}...)); err != nil {
			return err
		}
		return removeDir(path.Join([]string{cctx.HomeDir, cometNode.Config().DBPath}...))
	}

	return cctx, cleanup, nil
}

// StartGRPCServer starts the GRPC server using the provided application and
// config. A GRPC client connection to that server is also added to the client
// context. The returned function should be used to shutdown the server.
func StartGRPCServer(logger log.Logger, app srvtypes.Application, appCfg *srvconfig.Config, cctx Context) (*grpc.Server, Context, func() error, error) {
	emptycleanup := func() error { return nil }
	// Add the tx service in the gRPC router.
	app.RegisterTxService(cctx.Context)

	// Add the tendermint queries service in the gRPC router.
	app.RegisterTendermintService(cctx.Context)

	if a, ok := app.(srvtypes.Application); ok {
		a.RegisterNodeService(cctx.Context, *appCfg)
	}

	grpcSrv, err := srvgrpc.NewGRPCServer(cctx.Context, app, appCfg.GRPC)
	if err != nil {
		return nil, Context{}, emptycleanup, err
	}

	go func() {
		// StartGRPCServer is a blocking function, we need to run it in a go routine.
		if err := srvgrpc.StartGRPCServer(cctx.goContext, logger, appCfg.GRPC, grpcSrv); err != nil {
			panic(err)
		}
	}()

	nodeGRPCAddr := strings.Replace(appCfg.GRPC.Address, "0.0.0.0", "localhost", 1)
	conn, err := grpc.NewClient(
		nodeGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(codec.NewProtoCodec(cctx.InterfaceRegistry).GRPCCodec()),
		),
	)
	if err != nil {
		return nil, Context{}, emptycleanup, err
	}

	cctx.Context = cctx.WithGRPCClient(conn)

	return grpcSrv, cctx, func() error {
		grpcSrv.Stop()
		return nil
	}, nil
}

func StartAPIServer(app srvtypes.Application, appCfg srvconfig.Config, cctx Context, grpcSrv *grpc.Server) (*api.Server, error) {
	apiSrv := api.New(cctx.Context, log.NewNopLogger(), grpcSrv)
	app.RegisterAPIRoutes(apiSrv, appCfg.API)
	errCh := make(chan error)
	go func() {
		if err := apiSrv.Start(cctx.goContext, appCfg); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return nil, err
	case <-time.After(500 * time.Millisecond): // assume server started successfully
	}

	return apiSrv, nil
}
