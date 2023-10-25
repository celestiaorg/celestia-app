package node

import (
	"fmt"
	"net"
	"os"
	"time"

	"io"
	"path/filepath"

	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	rpc "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/client/local"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server/api"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	srvgrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	"github.com/cosmos/cosmos-sdk/telemetry"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/snapshots"
	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cast"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

var _ rpc.Client = (*Node)(nil)

type Node struct {
	*local.Local
	clientCtx client.Context
	consensus *node.Node
	app       servertypes.Application
	config    *Filesystem
	publishFn app.PublishFn
	closers   []Closer
	logger    log.Logger
}

type Closer func() error

func New(fs *Filesystem, publish app.PublishFn) (*Node, error) {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	logger = log.NewFilter(logger, log.AllowError())

	appServer, err := NewApp(fs, logger, publish)
	if err != nil {
		return nil, err
	}

	nodeKey, err := p2p.LoadOrGenNodeKey(fs.Consensus.NodeKeyFile())
	if err != nil {
		return nil, err
	}

	tmNode, err := node.NewNode(
		fs.Consensus,
		privval.LoadOrGenFilePV(fs.Consensus.PrivValidatorKeyFile(), fs.Consensus.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(appServer),
		node.DefaultGenesisDocProviderFunc(fs.Consensus),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(fs.Consensus.Instrumentation),
		logger,
	)

	rpcClient := local.New(tmNode)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	clientCtx := client.Context{}.
		WithCodec(encCfg.Codec).
		WithInterfaceRegistry(encCfg.InterfaceRegistry).
		WithTxConfig(encCfg.TxConfig).
		WithLegacyAmino(encCfg.Amino).
		WithHomeDir(fs.Consensus.RootDir)

	// Add the tx service to the gRPC router. We only need to register this
	// service if API or gRPC is enabled, and avoid doing so in the general
	// case, because it spawns a new local tendermint RPC client.
	if fs.App.API.Enable || fs.App.GRPC.Enable {
		// re-assign for making the client available below
		// do not use := to avoid shadowing clientCtx
		clientCtx = clientCtx.WithClient(rpcClient)

		// Add the tx service in the gRPC router.
		appServer.RegisterTxService(clientCtx)

		// Add the tendermint queries service in the gRPC router.
		appServer.RegisterTendermintService(clientCtx)

		// Add the node service queries to the grpc router.
		if a, ok := appServer.(servertypes.ApplicationQueryService); ok {
			a.RegisterNodeService(clientCtx)
		}
	}

	return &Node{
		consensus: tmNode,
		clientCtx: clientCtx,
		Local:     rpcClient,
		config:    fs,
		app:       appServer,
		logger:    logger,
	}, nil
}

func (n *Node) Start() error {
	var err error
	if err = n.consensus.Start(); err != nil {
		return err
	}
	n.addToCloser(func() error { return n.consensus.Stop() })

	if n.config.App.GRPC.Enable {
		n.startGRPCServer()
	}

	if n.config.App.API.Enable {
		n.startAPIServer()
	}

	// TODO: add the rosetta server

	return nil
}

func (n *Node) Stop() error {
	// we stop everything in the reverse order to
	// how they were started
	for i := len(n.closers) - 1; i >= 0; i-- {
		if err := n.closers[i](); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) Client() client.Context {
	return n.clientCtx
}

func (n *Node) startGRPCServer() error {
	grpcServer, err := srvgrpc.StartGRPCServer(n.clientCtx, n.app, n.config.App.GRPC)
	if err != nil {
		return err
	}
	n.addToCloser(func() error {
		grpcServer.Stop()
		return nil
	})

	_, port, err := net.SplitHostPort(n.config.App.GRPC.Address)
	if err != nil {
		return err
	}

	maxSendMsgSize := n.config.App.GRPC.MaxSendMsgSize
	if maxSendMsgSize == 0 {
		maxSendMsgSize = serverconfig.DefaultGRPCMaxSendMsgSize
	}

	maxRecvMsgSize := n.config.App.GRPC.MaxRecvMsgSize
	if maxRecvMsgSize == 0 {
		maxRecvMsgSize = serverconfig.DefaultGRPCMaxRecvMsgSize
	}

	grpcAddress := fmt.Sprintf("127.0.0.1:%s", port)

	// If grpc is enabled, configure grpc client for grpc gateway.
	grpcClient, err := grpc.Dial(
		grpcAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(codec.NewProtoCodec(n.clientCtx.InterfaceRegistry).GRPCCodec()),
			grpc.MaxCallRecvMsgSize(maxRecvMsgSize),
			grpc.MaxCallSendMsgSize(maxSendMsgSize),
		),
	)
	if err != nil {
		return err
	}

	n.clientCtx = n.clientCtx.WithGRPCClient(grpcClient)

	if n.config.App.GRPCWeb.Enable {
		grpcWebSrv, err := srvgrpc.StartGRPCWeb(grpcServer, *n.config.App)
		if err != nil {
			return err
		}
		n.addToCloser(func() error {
			return grpcWebSrv.Close()
		})
	}
	return nil
}

func (n *Node) startAPIServer() error {
	apiSrv := api.New(n.clientCtx, n.logger.With("module", "api-server"))
	n.app.RegisterAPIRoutes(apiSrv, n.config.App.API)
	if n.config.App.Telemetry.Enabled {
		metrics, err := telemetry.New(n.config.App.Telemetry)
		if err != nil {
			return err
		}
		apiSrv.SetTelemetry(metrics)
	}

	errCh := make(chan error)
	go func() {
		if err := apiSrv.Start(*n.config.App); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-time.After(servertypes.ServerStartTime):
	}

	n.addToCloser(func() error {
		return apiSrv.Close()
	})

	return nil
}

func (n *Node) addToCloser(closer Closer) {
	n.closers = append(n.closers, closer)
}

func NewApp(fs *Filesystem, logger log.Logger, publishFn app.PublishFn) (servertypes.Application, error) {
	db, err := dbm.NewGoLevelDB("application", fs.Consensus.DBDir())
	if err != nil {
		return nil, err
	}

	appOpts := app.NewKVAppOptions()
	appOpts.SetFromAppConfig(fs.App)
	appOpts.Set(flags.FlagHome, fs.Consensus.RootDir)

	return NewAppServer(logger, db, nil, appOpts), nil
}

func NewAppServer(logger log.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
	var cache sdk.MultiStorePersistentCache

	if cast.ToBool(appOpts.Get(server.FlagInterBlockCache)) {
		cache = store.NewCommitKVStoreCacheManager()
	}

	skipUpgradeHeights := make(map[int64]bool)
	for _, h := range cast.ToIntSlice(appOpts.Get(server.FlagUnsafeSkipUpgrades)) {
		skipUpgradeHeights[int64(h)] = true
	}

	pruningOpts, err := server.GetPruningOptionsFromFlags(appOpts)
	if err != nil {
		panic(err)
	}

	// Add snapshots
	snapshotDir := filepath.Join(cast.ToString(appOpts.Get(flags.FlagHome)), "data", "snapshots")
	//nolint: staticcheck
	snapshotDB, err := sdk.NewLevelDB("metadata", snapshotDir)
	if err != nil {
		panic(err)
	}
	snapshotStore, err := snapshots.NewStore(snapshotDB, snapshotDir)
	if err != nil {
		panic(err)
	}

	return app.New(
		logger, db, traceStore, true, skipUpgradeHeights,
		cast.ToString(appOpts.Get(flags.FlagHome)),
		cast.ToUint(appOpts.Get(server.FlagInvCheckPeriod)),
		encoding.MakeConfig(app.ModuleEncodingRegisters...), // Ideally, we would reuse the one created by NewRootCmd.
		appOpts,
		baseapp.SetPruning(pruningOpts),
		baseapp.SetMinGasPrices(cast.ToString(appOpts.Get(server.FlagMinGasPrices))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOpts.Get(server.FlagMinRetainBlocks))),
		baseapp.SetHaltHeight(cast.ToUint64(appOpts.Get(server.FlagHaltHeight))),
		baseapp.SetHaltTime(cast.ToUint64(appOpts.Get(server.FlagHaltTime))),
		baseapp.SetInterBlockCache(cache),
		baseapp.SetTrace(cast.ToBool(appOpts.Get(server.FlagTrace))),
		baseapp.SetIndexEvents(cast.ToStringSlice(appOpts.Get(server.FlagIndexEvents))),
		baseapp.SetSnapshot(snapshotStore, snapshottypes.NewSnapshotOptions(cast.ToUint64(appOpts.Get(server.FlagStateSyncSnapshotInterval)), cast.ToUint32(appOpts.Get(server.FlagStateSyncSnapshotKeepRecent)))),
		func(b *baseapp.BaseApp) {
			b.SetProtocolVersion(appconsts.LatestVersion)
		},
	)
}
