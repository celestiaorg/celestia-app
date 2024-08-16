package testnode

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server/api"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	srvgrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/rpc/client/local"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// noOpCleanup is a function that conforms to the cleanup function signature and
// performs no operation.
var noOpCleanup = func() error { return nil }

// StartNode starts the tendermint node along with a local core rpc client. The
// rpc is returned via the client.Context. The function returned should be
// called during cleanup to teardown the node, core client, along with canceling
// the internal context.Context in the returned Context.
func StartNode(tmNode *node.Node, cctx Context) (Context, func() error, error) {
	if err := tmNode.Start(); err != nil {
		return cctx, noOpCleanup, err
	}

	coreClient := local.New(tmNode)

	cctx.Context = cctx.WithClient(coreClient)
	cleanup := func() error {
		err := tmNode.Stop()
		if err != nil {
			return err
		}
		tmNode.Wait()
		if err = removeDir(path.Join([]string{cctx.HomeDir, "config"}...)); err != nil {
			return err
		}
		return removeDir(path.Join([]string{cctx.HomeDir, tmNode.Config().DBPath}...))
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

	if a, ok := app.(srvtypes.ApplicationQueryService); ok {
		a.RegisterNodeService(cctx.Context)
	}

	grpcSrv, err := srvgrpc.StartGRPCServer(cctx.Context, app, appCfg.GRPC)
	if err != nil {
		return Context{}, emptycleanup, err
	}

	nodeGRPCAddr := strings.Replace(appCfg.GRPC.Address, "0.0.0.0", "localhost", 1)
	conn, err := grpc.NewClient(
		nodeGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(codec.NewProtoCodec(cctx.InterfaceRegistry).GRPCCodec()),
		),
	)
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
	appCfg.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", mustGetFreePort())
	appCfg.API.Address = fmt.Sprintf("tcp://127.0.0.1:%d", mustGetFreePort())
	appCfg.MinGasPrices = fmt.Sprintf("%v%s", appconsts.DefaultMinGasPrice, appconsts.BondDenom)
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
		path := path.Join([]string{rootDir, d.Name()}...)
		err := os.RemoveAll(path)
		if err != nil {
			return err
		}
	}
	return os.RemoveAll(rootDir)
}

func StartAPIServer(app srvtypes.Application, appCfg srvconfig.Config, cctx Context) (*api.Server, error) {
	apiSrv := api.New(cctx.Context, log.NewNopLogger())
	app.RegisterAPIRoutes(apiSrv, appCfg.API)
	errCh := make(chan error)
	go func() {
		if err := apiSrv.Start(appCfg); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return nil, err

	case <-time.After(srvtypes.ServerStartTime): // assume server started successfully
	}
	return apiSrv, nil
}
