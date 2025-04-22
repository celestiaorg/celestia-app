package cmd

// NOTE: This file was copy paste forked from the sdk in order to modify the
// start command flag.

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servergrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	"github.com/cosmos/cosmos-sdk/server/rosetta"
	crgserver "github.com/cosmos/cosmos-sdk/server/rosetta/lib/server"
	srvrtypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"
	tmserver "github.com/tendermint/tendermint/abci/server"
	cmtcmd "github.com/tendermint/tendermint/cmd/cometbft/commands"
	tmos "github.com/tendermint/tendermint/libs/os"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/rpc/client/local"
	dbm "github.com/tendermint/tm-db"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// Tendermint full-node start flags
	flagWithTendermint = "with-tendermint"
	flagAddress        = "address"
	flagTransport      = "transport"
	flagTraceStore     = "trace-store"
	flagCPUProfile     = "cpu-profile"

	FlagForceNoBBR = "force-no-bbr"

	// gRPC-related flags
	flagGRPCOnly       = "grpc-only"
	flagGRPCEnable     = "grpc.enable"
	flagGRPCAddress    = "grpc.address"
	flagGRPCWebEnable  = "grpc-web.enable"
	flagGRPCWebAddress = "grpc-web.address"
)

// startCmd runs the service passed in, either stand-alone or in-process with
// Tendermint.
func startCmd(appCreator srvrtypes.AppCreator, defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Run the full node",
		Long: `Run the full node application with Tendermint in or out of process. By
default, the application will run with Tendermint in process.

Pruning options can be provided via the '--pruning' flag or alternatively with '--pruning-keep-recent', and
'pruning-interval' together.

For '--pruning' the options are as follows:

default: the last 362880 states are kept, pruning at 10 block intervals
nothing: all historic states will be saved, nothing will be deleted (i.e. archiving node)
everything: 2 latest states will be kept; pruning at 10 block intervals.
custom: allow pruning options to be manually specified through 'pruning-keep-recent', and 'pruning-interval'

Node halting configurations exist in the form of two flags: '--halt-height' and '--halt-time'. During
the ABCI Commit phase, the node will check if the current block height is greater than or equal to
the halt-height or if the current block time is greater than or equal to the halt-time. If so, the
node will attempt to gracefully shutdown and the block will not be committed. In addition, the node
will not be able to commit subsequent blocks.

For profiling and benchmarking purposes, CPU profiling can be enabled via the '--cpu-profile' flag
which accepts a path for the resulting pprof file.

The node may be started in a 'query only' mode where only the gRPC and JSON HTTP
API services are enabled via the 'grpc-only' flag. In this mode, Tendermint is
bypassed and can be used when legacy queries are needed after an on-chain upgrade
is performed. Note, when enabled, gRPC will also be automatically enabled.
`,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)

			// Bind flags to the Context's Viper so the app construction can set
			// options accordingly.
			if err := serverCtx.Viper.BindPFlags(cmd.Flags()); err != nil {
				return err
			}

			_, err := server.GetPruningOptionsFromFlags(serverCtx.Viper)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := checkBBR(cmd)
			if err != nil {
				return err
			}

			serverCtx := server.GetServerContextFromCmd(cmd)
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			if contains(appconsts.PublicNetworks, clientCtx.ChainID) && serverCtx.Viper.GetDuration(TimeoutCommitFlag) != 0 {
				return fmt.Errorf("the --timeout-commit flag was used on %v but it is unsupported on public networks: %v. The --timeout-commit flag should only be used on private testnets", clientCtx.ChainID, strings.Join(appconsts.PublicNetworks, ", "))
			}

			switch clientCtx.ChainID {
			case appconsts.ArabicaChainID:
				serverCtx.Logger.Info(fmt.Sprintf("Since the chainID is %v, configuring the default v2 upgrade height to %v", appconsts.ArabicaChainID, appconsts.ArabicaUpgradeHeightV2))
				serverCtx.Viper.SetDefault(UpgradeHeightFlag, appconsts.ArabicaUpgradeHeightV2)
			case appconsts.MochaChainID:
				serverCtx.Logger.Info(fmt.Sprintf("Since the chainID is %v, configuring the default v2 upgrade height to %v", appconsts.MochaChainID, appconsts.MochaUpgradeHeightV2))
				serverCtx.Viper.SetDefault(UpgradeHeightFlag, appconsts.MochaUpgradeHeightV2)
			case appconsts.MainnetChainID:
				serverCtx.Logger.Info(fmt.Sprintf("Since the chainID is %v, configuring the default v2 upgrade height to %v", appconsts.MainnetChainID, appconsts.MainnetUpgradeHeightV2))
				serverCtx.Viper.SetDefault(UpgradeHeightFlag, appconsts.MainnetUpgradeHeightV2)
			default:
				serverCtx.Logger.Info(fmt.Sprintf("No default value exists for the v2 upgrade height when the chainID is %v", clientCtx.ChainID))
			}

			withTM, _ := cmd.Flags().GetBool(flagWithTendermint)
			if !withTM {
				serverCtx.Logger.Info("starting ABCI without Tendermint")
				return wrapCPUProfile(serverCtx, func() error {
					return startStandAlone(serverCtx, clientCtx, appCreator)
				})
			}

			// amino is needed here for backwards compatibility of REST routes
			err = wrapCPUProfile(serverCtx, func() error {
				return startInProcess(serverCtx, clientCtx, appCreator)
			})
			errCode, ok := err.(server.ErrorCode)
			if !ok {
				return err
			}

			serverCtx.Logger.Debug(fmt.Sprintf("received quit signal: %d", errCode.Code))
			return nil
		},
	}

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	cmd.Flags().Bool(flagWithTendermint, true, "Run abci app embedded in-process with tendermint")
	cmd.Flags().String(flagAddress, "tcp://0.0.0.0:26658", "Listen address")
	cmd.Flags().String(flagTransport, "socket", "Transport protocol: socket, grpc")
	cmd.Flags().String(flagTraceStore, "", "Enable KVStore tracing to an output file")
	cmd.Flags().String(server.FlagMinGasPrices, "", "Minimum gas prices to accept for transactions; Any fee in a tx must meet this minimum (e.g. 0.01photino;0.0001stake)")
	cmd.Flags().IntSlice(server.FlagUnsafeSkipUpgrades, []int{}, "Skip a set of upgrade heights to continue the old binary")
	cmd.Flags().Uint64(server.FlagHaltHeight, 0, "Block height at which to gracefully halt the chain and shutdown the node")
	cmd.Flags().Uint64(server.FlagHaltTime, 0, "Minimum block time (in Unix seconds) at which to gracefully halt the chain and shutdown the node")
	cmd.Flags().Bool(server.FlagInterBlockCache, true, "Enable inter-block caching")
	cmd.Flags().String(flagCPUProfile, "", "Enable CPU profiling and write to the provided file")
	cmd.Flags().Bool(server.FlagTrace, false, "Provide full stack traces for errors in ABCI Log")
	cmd.Flags().String(server.FlagPruning, pruningtypes.PruningOptionDefault, "Pruning strategy (default|nothing|everything|custom)")
	cmd.Flags().Uint64(server.FlagPruningKeepRecent, 0, "Number of recent heights to keep on disk (ignored if pruning is not 'custom')")
	cmd.Flags().Uint64(server.FlagPruningInterval, 0, "Height interval at which pruned heights are removed from disk (ignored if pruning is not 'custom')")
	cmd.Flags().Uint(server.FlagInvCheckPeriod, 0, "Assert registered invariants every N blocks")
	cmd.Flags().Uint64(server.FlagMinRetainBlocks, 0, "Minimum block height offset during ABCI commit to prune Tendermint blocks")
	cmd.Flags().Bool(FlagForceNoBBR, false, "bypass the requirement to use bbr locally")

	cmd.Flags().Bool(server.FlagAPIEnable, false, "Define if the API server should be enabled")
	cmd.Flags().Bool(server.FlagAPISwagger, false, "Define if swagger documentation should automatically be registered (Note: the API must also be enabled)")
	cmd.Flags().String(server.FlagAPIAddress, serverconfig.DefaultAPIAddress, "the API server address to listen on")
	cmd.Flags().Uint(server.FlagAPIMaxOpenConnections, 1000, "Define the number of maximum open connections")
	cmd.Flags().Uint(server.FlagRPCReadTimeout, 10, "Define the Tendermint RPC read timeout (in seconds)")
	cmd.Flags().Uint(server.FlagRPCWriteTimeout, 0, "Define the Tendermint RPC write timeout (in seconds)")
	cmd.Flags().Uint(server.FlagRPCMaxBodyBytes, 1000000, "Define the Tendermint maximum response body (in bytes)")
	cmd.Flags().Bool(server.FlagAPIEnableUnsafeCORS, false, "Define if CORS should be enabled (unsafe - use it at your own risk)")

	cmd.Flags().Bool(flagGRPCOnly, false, "Start the node in gRPC query only mode (no Tendermint process is started)")
	cmd.Flags().Bool(flagGRPCEnable, true, "Define if the gRPC server should be enabled")
	cmd.Flags().String(flagGRPCAddress, serverconfig.DefaultGRPCAddress, "the gRPC server address to listen on")

	cmd.Flags().Bool(flagGRPCWebEnable, true, "Define if the gRPC-Web server should be enabled. (Note: gRPC must also be enabled)")
	cmd.Flags().String(flagGRPCWebAddress, serverconfig.DefaultGRPCWebAddress, "The gRPC-Web server address to listen on")

	cmd.Flags().Uint64(server.FlagStateSyncSnapshotInterval, 0, "State sync snapshot interval")
	cmd.Flags().Uint32(server.FlagStateSyncSnapshotKeepRecent, 2, "State sync snapshot to keep")

	cmd.Flags().Bool(server.FlagDisableIAVLFastNode, false, "Disable fast node for IAVL tree")

	// add support for all Tendermint-specific command line options
	cmtcmd.AddNodeFlags(cmd)
	return cmd
}

func startStandAlone(ctx *server.Context, clientCtx client.Context, appCreator srvrtypes.AppCreator) error {
	addr := ctx.Viper.GetString(flagAddress)
	transport := ctx.Viper.GetString(flagTransport)
	home := ctx.Viper.GetString(flags.FlagHome)

	db, err := openDB(home, server.GetAppDBBackend(ctx.Viper))
	if err != nil {
		return err
	}

	traceWriterFile := ctx.Viper.GetString(flagTraceStore)
	traceWriter, err := openTraceWriter(traceWriterFile)
	if err != nil {
		return err
	}

	app := appCreator(ctx.Logger, db, traceWriter, ctx.Viper)

	config, err := serverconfig.GetConfig(ctx.Viper)
	if err != nil {
		return err
	}

	_, err = startTelemetry(config)
	if err != nil {
		return err
	}

	// Add the tx service to the gRPC router. We only need to register this
	// service if API or gRPC is enabled.
	if config.API.Enable || config.GRPC.Enable {
		app.RegisterTxService(clientCtx)
		app.RegisterTendermintService(clientCtx)

		if a, ok := app.(srvrtypes.ApplicationQueryService); ok {
			a.RegisterNodeService(clientCtx)
		}
	}

	metrics, err := startTelemetry(config)
	if err != nil {
		return err
	}

	var (
		apiSrv     *api.Server
		grpcSrv    *grpc.Server
		grpcWebSrv *http.Server
	)

	if config.API.Enable {
		apiSrv, clientCtx, err = startAPIServer(ctx, clientCtx, app, config, metrics)
		if err != nil {
			ctx.Logger.Error("failed to start api server: ", err)
			return err
		}
		defer func() {
			if err := apiSrv.Close(); err != nil {
				ctx.Logger.Error("failed to close api server: ", err)
			}
		}()
	}

	if config.GRPC.Enable {
		grpcSrv, _, err = setupGRPCServer(ctx, clientCtx, app, config, true)
		if err != nil {
			ctx.Logger.Error("failed to start grpc server: ", err)
			return err
		}
		defer grpcSrv.Stop()
	}

	if grpcSrv != nil && config.GRPCWeb.Enable {
		grpcWebSrv, err = servergrpc.StartGRPCWeb(grpcSrv, config)
		if err != nil {
			ctx.Logger.Error("failed to start grpc-web http server: ", err)
			return err
		}
		defer func() {
			if err := grpcWebSrv.Close(); err != nil {
				ctx.Logger.Error("failed to close grpc-web http server: ", err)
			}
		}()
	}

	svr, err := tmserver.NewServer(addr, transport, app)
	if err != nil {
		return fmt.Errorf("error creating listener: %v", err)
	}

	svr.SetLogger(ctx.Logger.With("module", "abci-server"))

	err = svr.Start()
	if err != nil {
		tmos.Exit(err.Error())
	}

	defer func() {
		if err = svr.Stop(); err != nil {
			tmos.Exit(err.Error())
		}

		if err = app.Close(); err != nil {
			tmos.Exit(err.Error())
		}
	}()

	// Wait for SIGINT or SIGTERM signal
	return server.WaitForQuitSignals()
}

func startInProcess(ctx *server.Context, clientCtx client.Context, appCreator srvrtypes.AppCreator) error {
	cfg := ctx.Config
	home := cfg.RootDir

	db, err := openDB(home, server.GetAppDBBackend(ctx.Viper))
	if err != nil {
		return err
	}

	traceWriterFile := ctx.Viper.GetString(flagTraceStore)
	traceWriter, err := openTraceWriter(traceWriterFile)
	if err != nil {
		return err
	}

	config, err := serverconfig.GetConfig(ctx.Viper)
	if err != nil {
		return err
	}

	if err := config.ValidateBasic(); err != nil {
		return err
	}

	app := appCreator(ctx.Logger, db, traceWriter, ctx.Viper)

	nodeKey, err := p2p.LoadOrGenNodeKey(cfg.NodeKeyFile())
	if err != nil {
		return err
	}

	genDocProvider := node.DefaultGenesisDocProviderFunc(cfg)

	var (
		tmNode   *node.Node
		gRPCOnly = ctx.Viper.GetBool(flagGRPCOnly)
	)

	if gRPCOnly {
		ctx.Logger.Info("starting node in gRPC only mode; Tendermint is disabled")
		config.GRPC.Enable = true
	} else {
		ctx.Logger.Info("starting node with ABCI Tendermint in-process")

		tmNode, err = node.NewNode(
			cfg,
			privval.LoadOrGenFilePV(cfg.PrivValidatorKeyFile(), cfg.PrivValidatorStateFile()),
			nodeKey,
			proxy.NewLocalClientCreator(app),
			genDocProvider,
			node.DefaultDBProvider,
			node.DefaultMetricsProvider(cfg.Instrumentation),
			ctx.Logger,
		)
		if err != nil {
			return err
		}
		if err := tmNode.Start(); err != nil {
			return err
		}
	}

	// Add the tx service to the gRPC router. We only need to register this
	// service if API or gRPC is enabled, and avoid doing so in the general
	// case, because it spawns a new local tendermint RPC client.
	if (config.API.Enable || config.GRPC.Enable) && tmNode != nil {
		// re-assign for making the client available below
		// do not use := to avoid shadowing clientCtx
		clientCtx = clientCtx.WithClient(local.New(tmNode))

		app.RegisterTxService(clientCtx)
		app.RegisterTendermintService(clientCtx)

		if a, ok := app.(srvrtypes.ApplicationQueryService); ok {
			a.RegisterNodeService(clientCtx)
		}
	}

	metrics, err := startTelemetry(config)
	if err != nil {
		return err
	}

	var (
		apiSrv     *api.Server
		grpcSrv    *grpc.Server
		grpcWebSrv *http.Server
	)

	if config.API.Enable {
		apiSrv, clientCtx, err = startAPIServer(ctx, clientCtx, app, config, metrics)
		if err != nil {
			ctx.Logger.Error("failed to start api server: ", err)
			return err
		}
		defer func() {
			if err := apiSrv.Close(); err != nil {
				ctx.Logger.Error("failed to close api server: ", err)
			}
		}()
	}

	if config.GRPC.Enable {
		grpcSrv, clientCtx, err = setupGRPCServer(ctx, clientCtx, app, config, false)
		if err != nil {
			ctx.Logger.Error("failed to start grpc server: ", err)
			return err
		}
		defer grpcSrv.Stop()
	}

	if grpcSrv != nil && config.GRPCWeb.Enable {
		grpcWebSrv, err = servergrpc.StartGRPCWeb(grpcSrv, config)
		if err != nil {
			ctx.Logger.Error("failed to start grpc-web http server: ", err)
			return err
		}
		defer func() {
			if err := grpcWebSrv.Close(); err != nil {
				ctx.Logger.Error("failed to close grpc-web http server: ", err)
			}
		}()
	}

	// At this point it is safe to block the process if we're in gRPC only mode as
	// we do not need to start Rosetta or handle any Tendermint related processes.
	if gRPCOnly {
		// wait for signal capture and gracefully return
		return server.WaitForQuitSignals()
	}

	var rosettaSrv crgserver.Server
	if config.Rosetta.Enable {
		offlineMode := config.Rosetta.Offline

		// If GRPC is not enabled rosetta cannot work in online mode, so it works in
		// offline mode.
		if !config.GRPC.Enable {
			offlineMode = true
		}

		minGasPrices, err := sdktypes.ParseDecCoins(config.MinGasPrices)
		if err != nil {
			ctx.Logger.Error("failed to parse minimum-gas-prices: ", err)
			return err
		}

		conf := &rosetta.Config{
			Blockchain:          config.Rosetta.Blockchain,
			Network:             config.Rosetta.Network,
			TendermintRPC:       ctx.Config.RPC.ListenAddress,
			GRPCEndpoint:        config.GRPC.Address,
			Addr:                config.Rosetta.Address,
			Retries:             config.Rosetta.Retries,
			Offline:             offlineMode,
			GasToSuggest:        config.Rosetta.GasToSuggest,
			EnableFeeSuggestion: config.Rosetta.EnableFeeSuggestion,
			GasPrices:           minGasPrices.Sort(),
			Codec:               clientCtx.Codec.(*codec.ProtoCodec),
			InterfaceRegistry:   clientCtx.InterfaceRegistry,
		}

		rosettaSrv, err = rosetta.ServerFromConfig(conf)
		if err != nil {
			return err
		}

		errCh := make(chan error)
		go func() {
			if err := rosettaSrv.Start(); err != nil {
				errCh <- err
			}
		}()

		select {
		case err := <-errCh:
			return err

		case <-time.After(srvrtypes.ServerStartTime): // assume server started successfully
		}
	}

	defer func() {
		if tmNode != nil && tmNode.IsRunning() {
			_ = tmNode.Stop()
			_ = app.Close()
		}
		ctx.Logger.Info("exiting...")
	}()

	// wait for signal capture and gracefully return
	return server.WaitForQuitSignals()
}

// setupGRPCServer initializes and starts the gRPC server with the given configuration and returns the server instance.
// returns the gRPC server, updated client context, and an error if any step fails during setup.
func setupGRPCServer(ctx *server.Context, clientCtx client.Context, app srvrtypes.Application, config serverconfig.Config, isStandalone bool) (*grpc.Server, client.Context, error) {
	grpcSrv, err := servergrpc.StartGRPCServer(clientCtx, app, config.GRPC, isStandalone, ctx.Config.RPC.GRPCListenAddress)
	if err != nil {
		return nil, clientCtx, err
	}

	_, _, err = net.SplitHostPort(config.GRPC.Address)
	if err != nil {
		return nil, clientCtx, err
	}

	maxSendMsgSize := config.GRPC.MaxSendMsgSize
	if maxSendMsgSize == 0 {
		maxSendMsgSize = serverconfig.DefaultGRPCMaxSendMsgSize
	}

	maxRecvMsgSize := config.GRPC.MaxRecvMsgSize
	if maxRecvMsgSize == 0 {
		maxRecvMsgSize = serverconfig.DefaultGRPCMaxRecvMsgSize
	}

	// If grpc is enabled, configure grpc client for grpc gateway.
	grpcClient, err := grpc.NewClient(
		config.GRPC.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(codec.NewProtoCodec(clientCtx.InterfaceRegistry).GRPCCodec()),
			grpc.MaxCallRecvMsgSize(maxRecvMsgSize),
			grpc.MaxCallSendMsgSize(maxSendMsgSize),
		),
	)
	if err != nil {
		return nil, clientCtx, err
	}

	clientCtx = clientCtx.WithGRPCClient(grpcClient)
	ctx.Logger.Debug("grpc client assigned to client context", "target", config.GRPC.Address)

	return grpcSrv, clientCtx, nil
}

// startAPIServer initializes and starts the API server, setting up routes and telemetry if enabled, and returns the server.
func startAPIServer(ctx *server.Context, clientCtx client.Context, app srvrtypes.Application, config serverconfig.Config, metrics *telemetry.Metrics) (*api.Server, client.Context, error) {
	genDocProvider := node.DefaultGenesisDocProviderFunc(ctx.Config)
	genDoc, err := genDocProvider()
	if err != nil {
		return nil, clientCtx, err
	}

	clientCtx = clientCtx.WithHomeDir(ctx.Config.RootDir).WithChainID(genDoc.ChainID)

	apiSrv := api.New(clientCtx, ctx.Logger.With("module", "api-server"))
	app.RegisterAPIRoutes(apiSrv, config.API)

	if config.Telemetry.Enabled {
		apiSrv.SetTelemetry(metrics)
	}

	errCh := make(chan error)
	go func() {
		if err := apiSrv.Start(config); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return nil, clientCtx, err
	case <-time.After(srvrtypes.ServerStartTime):
		return apiSrv, clientCtx, nil
	}
}

func startTelemetry(cfg serverconfig.Config) (*telemetry.Metrics, error) {
	if !cfg.Telemetry.Enabled {
		return nil, nil
	}
	return telemetry.New(cfg.Telemetry)
}

// wrapCPUProfile runs callback in a goroutine, then wait for quit signals.
func wrapCPUProfile(ctx *server.Context, callback func() error) error {
	if cpuProfile := ctx.Viper.GetString(flagCPUProfile); cpuProfile != "" {
		f, err := os.Create(cpuProfile)
		if err != nil {
			return err
		}

		ctx.Logger.Info("starting CPU profiler", "profile", cpuProfile)
		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}

		defer func() {
			ctx.Logger.Info("stopping CPU profiler", "profile", cpuProfile)
			pprof.StopCPUProfile()
			if err := f.Close(); err != nil {
				ctx.Logger.Info("failed to close cpu-profile file", "profile", cpuProfile, "err", err.Error())
			}
		}()
	}

	errCh := make(chan error)
	go func() {
		errCh <- callback()
	}()

	select {
	case err := <-errCh:
		return err

	case <-time.After(srvrtypes.ServerStartTime):
	}

	return server.WaitForQuitSignals()
}

func addCommands(
	rootCmd *cobra.Command,
	defaultNodeHome string,
	appCreator srvrtypes.AppCreator,
	appExport srvrtypes.AppExporter,
	addStartFlags srvrtypes.ModuleInitFlags,
) {
	tendermintCmd := &cobra.Command{
		Use:     "tendermint",
		Aliases: []string{"comet", "cometbft"},
		Short:   "Tendermint subcommands",
	}

	tendermintCmd.AddCommand(
		server.ShowNodeIDCmd(),
		server.ShowValidatorCmd(),
		server.ShowAddressCmd(),
		server.VersionCmd(),
		cmtcmd.ResetAllCmd,
		cmtcmd.ResetStateCmd,
		server.BootstrapStateCmd(appCreator),
	)

	startCmd := startCmd(appCreator, defaultNodeHome)
	addStartFlags(startCmd)

	rootCmd.AddCommand(
		startCmd,
		tendermintCmd,
		server.ExportCmd(appExport, defaultNodeHome),
		version.NewVersionCommand(),
		server.NewRollbackCmd(appCreator, defaultNodeHome),
	)
}

// checkBBR checks if BBR is enabled.
func checkBBR(command *cobra.Command) error {
	const (
		warning = `
The BBR (Bottleneck Bandwidth and Round-trip propagation time) congestion control algorithm is not enabled in this system's kernel.
BBR is important for the performance of the p2p stack.

To enable BBR:
sudo modprobe tcp_bbr
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
sudo sysctl -p

Then verify BBR is enabled:
sysctl net.ipv4.tcp_congestion_control
or
cat /proc/sys/net/ipv4/tcp_congestion_control

This node will get worse p2p performance using a different congestion control algorithm.
If you need to bypass this check use the --force-no-bbr flag.
`
	)

	forceNoBBR, err := command.Flags().GetBool(FlagForceNoBBR)
	if err != nil {
		return err
	}
	if forceNoBBR {
		return nil
	}

	file, err := os.ReadFile("/proc/sys/net/ipv4/tcp_congestion_control")
	if err != nil {
		fmt.Print(warning)
		return fmt.Errorf("failed to read file '/proc/sys/net/ipv4/tcp_congestion_control' %w", err)
	}

	if !strings.Contains(string(file), "bbr") {
		fmt.Print(warning)
		return fmt.Errorf("BBR not enabled because output %v does not contain 'bbr'", string(file))
	}

	return nil
}

func openDB(rootDir string, backendType dbm.BackendType) (dbm.DB, error) {
	dataDir := filepath.Join(rootDir, "data")
	return dbm.NewDB("application", backendType, dataDir)
}

func openTraceWriter(traceWriterFile string) (w io.Writer, err error) {
	if traceWriterFile == "" {
		return nil, nil
	}
	return os.OpenFile(
		traceWriterFile,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE,
		0o666,
	)
}
