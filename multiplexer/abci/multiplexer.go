package abci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"cosmossdk.io/log"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	pvm "github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/rpc/client/local"
	db "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servergrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	servercmtlog "github.com/cosmos/cosmos-sdk/server/log"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/hashicorp/go-metrics"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/01builders/nova/appd"
	"github.com/01builders/nova/internal"
)

const (
	flagTraceStore = "trace-store"
	flagGRPCOnly   = "grpc-only"
)

// Multiplexer is responsible for managing multiple versions of applications and coordinating their lifecycle.
// It handles version switching between embedded and native applications.
// It manages configuration, connection setup, and cleanup functions for all associated services and resources.
type Multiplexer struct {
	logger log.Logger
	mu     sync.Mutex

	svrCtx *server.Context
	svrCfg serverconfig.Config
	// clientContext is used to configure the different services managed by the multiplexer.
	clientContext client.Context
	// appVersion is the current version application number.
	appVersion uint64
	// nextAppVersion this is updated based on the consensus params every FinalizeBlock.
	nextAppVersion uint64
	// started indicates if there is either an embedded app running, or a native one running.
	started bool
	// appCreator is a function type responsible for creating a new application instance.
	appCreator servertypes.AppCreator
	// nativeApp represents the instance of a native application.
	// the multiplexer either uses this instance, or the version set in activeVersion.
	nativeApp servertypes.Application
	// activeVersion is the currently active embedded version that is running.
	activeVersion Version
	// chainID is required as it needs to be propagated to the ABCI V1 connection.
	chainID string
	// cmNode is the comet node which has been created. A reference is required in order to establish
	// a local connection to it.
	cmNode *node.Node
	// versions is a list of versions which contain all embedded binaries.
	versions Versions
	// conn is a grpc client connection and used when creating remote ABCI connections.
	conn *grpc.ClientConn
	// cleanupFns is a list of functions which should execute upon cleanup of the multiplexer.
	// any returned errors are logged.
	cleanupFns []func() error
	// ctx is the context which is passed to the comet, grpc and api server starting functions.
	ctx context.Context
	// g is the waitgroup to which the comet, grpc and api server init functions are added to.
	g *errgroup.Group
}

// NewVersions returns a list of versions sorted by app version.
func NewVersions(v ...Version) (Versions, error) {
	versions := Versions(v)
	if err := versions.Validate(); err != nil {
		return nil, err
	}
	return versions.Sorted(), nil
}

// NewMultiplexer creates a new Multiplexer.
func NewMultiplexer(svrCtx *server.Context, svrCfg serverconfig.Config, clientCtx client.Context, appCreator servertypes.AppCreator, versions Versions, chainID string, applicationVersion uint64) (*Multiplexer, error) {
	if err := versions.Validate(); err != nil {
		return nil, fmt.Errorf("invalid versions: %w", err)
	}

	mp := &Multiplexer{
		svrCtx:        svrCtx,
		svrCfg:        svrCfg,
		clientContext: clientCtx,
		appCreator:    appCreator,
		logger:        svrCtx.Logger.With("multiplexer"),
		nativeApp:     nil, // app will be initialized if required by the multiplexer.
		versions:      versions,
		chainID:       chainID,
		appVersion:    applicationVersion,
		cleanupFns:    make([]func() error, 0),
	}

	return mp, nil
}

// isRunningNativeApp checks if the native application is currently running.
func (m *Multiplexer) isNativeApp() bool {
	return m.nativeApp != nil
}

// isEmbeddedApp checks if an embedded application is currently running.
func (m *Multiplexer) isEmbeddedApp() bool {
	return !m.isNativeApp()
}

// isGrpcOnly checks if the GRPC-only mode is enabled using the configuration flag.
func (m *Multiplexer) isGrpcOnly() bool {
	return m.svrCtx.Viper.GetBool(flagGRPCOnly)
}

// registerCleanupFn enables the registration of additional cleanup functions that get called during Cleanup
func (m *Multiplexer) registerCleanupFn(cleanUpFn func() error) {
	m.cleanupFns = append(m.cleanupFns, cleanUpFn)
}

func (m *Multiplexer) Start() error {
	m.g, m.ctx = getCtx(m.svrCtx, true)

	emitServerInfoMetrics()

	// startApp starts the underlying application, either native or embedded.
	if err := m.startApp(); err != nil {
		return err
	}

	if m.isGrpcOnly() {
		m.logger.Info("starting node in gRPC only mode; CometBFT is disabled")
		m.svrCfg.GRPC.Enable = true
	} else {
		m.logger.Info("starting comet node")
		if err := m.startCmtNode(); err != nil {
			return err
		}
	}

	if m.isEmbeddedApp() {
		m.logger.Debug("using embedded app, not continuing with grpc or api servers")
		return m.g.Wait()
	}

	if err := m.enableGRPCAndAPIServers(m.nativeApp); err != nil {
		return err
	}

	// wait for signal capture and gracefully return
	// we are guaranteed to be waiting for the "ListenForQuitSignals" goroutine.
	return m.g.Wait()
}

// enableGRPCAndAPIServers enables the gRPC and API servers for the provided application if configured to do so.
// It registers transaction, Tendermint, and node services, and starts the gRPC and API servers if enabled.
func (m *Multiplexer) enableGRPCAndAPIServers(app servertypes.Application) error {
	if app == nil {
		return fmt.Errorf("unable to enable grpc and api servers, app is nil")
	}
	// if we are running natively and have specified to enable gRPC or API servers
	// we need to register the relevant services.
	if m.svrCfg.API.Enable || m.svrCfg.GRPC.Enable {
		m.logger.Debug("registering services and local comet client")
		m.clientContext = m.clientContext.WithClient(local.New(m.cmNode))
		app.RegisterTxService(m.clientContext)
		app.RegisterTendermintService(m.clientContext)
		app.RegisterNodeService(m.clientContext, m.svrCfg)
	}

	// startGRPCServer the grpc server in the case of a native app. If using an embedded app
	// it will use that instead.
	if m.svrCfg.GRPC.Enable {
		grpcServer, clientContext, err := m.startGRPCServer()
		if err != nil {
			return err
		}
		m.clientContext = clientContext // update client context with grpc

		// startAPIServer starts the api server for a native app. If using an embedded app
		// it will use that instead.
		if m.svrCfg.API.Enable {
			metrics, err := startTelemetry(m.svrCfg)
			if err != nil {
				return err
			}

			if err := m.startAPIServer(grpcServer, metrics); err != nil {
				return err
			}
		}
	}
	return nil
}

// startApp starts either the native app, or an embedded app.
func (m *Multiplexer) startApp() error {
	// prepare correct version
	currentVersion, err := m.versions.GetForAppVersion(m.appVersion)
	if err != nil && errors.Is(err, ErrNoVersionFound) {
		// no version found, assume latest
		if _, err := m.startNativeApp(); err != nil {
			return fmt.Errorf("failed to start native app: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get app for version %d: %w", m.appVersion, err)
	}

	// start the correct version
	if currentVersion.Appd == nil {
		return fmt.Errorf("appd is nil for version %d", m.appVersion)
	}

	if currentVersion.Appd.Pid() == appd.AppdStopped {
		programArgs := os.Args
		if len(os.Args) > 2 {
			programArgs = os.Args[2:] // remove 'appd start' args
		}

		// start an embedded app.
		m.logger.Debug("starting embedded app", "app_version", currentVersion.AppVersion, "args", currentVersion.GetStartArgs(programArgs))
		if err := currentVersion.Appd.Start(currentVersion.GetStartArgs(programArgs)...); err != nil {
			return fmt.Errorf("failed to start app: %w", err)
		}

		if currentVersion.Appd.Pid() == appd.AppdStopped { // should never happen
			return fmt.Errorf("app failed to start")
		}

		m.started = true
		m.activeVersion = currentVersion
	}

	return m.initRemoteGrpcConn()
}

// initRemoteGrpcConn initializes a gRPC connection to the remote application client and configures transport credentials.
func (m *Multiplexer) initRemoteGrpcConn() error {
	// prepare remote app client
	const flagTMAddress = "address"
	tmAddress := m.svrCtx.Viper.GetString(flagTMAddress)
	if tmAddress == "" {
		tmAddress = "127.0.0.1:26658"
	}

	// remove tcp:// prefix if present
	tmAddress = strings.TrimPrefix(tmAddress, "tcp://")

	conn, err := grpc.NewClient(
		tmAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(math.MaxInt32),
			grpc.MaxCallRecvMsgSize(math.MaxInt32),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to prepare app connection: %w", err)
	}

	m.logger.Info("initialized remote app client", "address", tmAddress)
	m.conn = conn
	return nil
}

// startGRPCServer initializes and starts a gRPC server if enabled in the configuration, returning the server and updated context.
func (m *Multiplexer) startGRPCServer() (*grpc.Server, client.Context, error) {
	_, _, err := net.SplitHostPort(m.svrCfg.GRPC.Address)
	if err != nil {
		return nil, m.clientContext, err
	}

	maxSendMsgSize := m.svrCfg.GRPC.MaxSendMsgSize
	if maxSendMsgSize == 0 {
		maxSendMsgSize = serverconfig.DefaultGRPCMaxSendMsgSize
	}

	maxRecvMsgSize := m.svrCfg.GRPC.MaxRecvMsgSize
	if maxRecvMsgSize == 0 {
		maxRecvMsgSize = serverconfig.DefaultGRPCMaxRecvMsgSize
	}

	// if gRPC is enabled, configure gRPC client for gRPC gateway
	grpcClient, err := grpc.NewClient(
		m.svrCfg.GRPC.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(codec.NewProtoCodec(m.clientContext.InterfaceRegistry).GRPCCodec()),
			grpc.MaxCallRecvMsgSize(maxRecvMsgSize),
			grpc.MaxCallSendMsgSize(maxSendMsgSize),
		),
	)
	if err != nil {
		return nil, m.clientContext, err
	}

	m.clientContext = m.clientContext.WithGRPCClient(grpcClient)
	m.logger.Debug("gRPC client assigned to client context", "target", m.svrCfg.GRPC.Address)
	grpcSrv, err := servergrpc.NewGRPCServer(m.clientContext, m.nativeApp, m.svrCfg.GRPC)
	if err != nil {
		return nil, m.clientContext, err
	}

	// Start the gRPC server in a goroutine. Note, the provided ctx will ensure
	// that the server is gracefully shut down.
	m.g.Go(func() error {
		return servergrpc.StartGRPCServer(m.ctx, m.logger.With(log.ModuleKey, "grpc-server"), m.svrCfg.GRPC, grpcSrv)
	})

	m.conn = grpcClient
	return grpcSrv, m.clientContext, nil
}

// startAPIServer initializes and starts the API server, setting up routes, telemetry, and running it within an error group.
func (m *Multiplexer) startAPIServer(grpcSrv *grpc.Server, metrics *telemetry.Metrics) error {
	if m.isEmbeddedApp() {
		return fmt.Errorf("cannot start api server for embedded app")
	}

	m.clientContext = m.clientContext.WithHomeDir(m.svrCtx.Config.RootDir)

	apiSrv := api.New(m.clientContext, m.logger.With(log.ModuleKey, "api-server"), grpcSrv)
	m.nativeApp.RegisterAPIRoutes(apiSrv, m.svrCfg.API)

	if m.svrCfg.Telemetry.Enabled {
		apiSrv.SetTelemetry(metrics)
	}

	m.logger.Debug("starting api server")
	m.g.Go(func() error {
		return apiSrv.Start(m.ctx, m.svrCfg)
	})
	return nil
}

// startNativeApp starts a native app.
func (m *Multiplexer) startNativeApp() (servertypes.Application, error) {
	traceWriter, traceCleanupFn, err := setupTraceWriter(m.svrCtx)
	if err != nil {
		return nil, err
	}
	m.registerCleanupFn(func() error {
		traceCleanupFn()
		return nil
	})

	home := m.svrCtx.Config.RootDir
	db, err := openDB(home, server.GetAppDBBackend(m.svrCtx.Viper))
	if err != nil {
		return nil, err
	}

	m.logger.Debug("creating native app", "app_version", m.appVersion)
	m.nativeApp = m.appCreator(m.logger, db, traceWriter, m.svrCtx.Viper)
	m.started = true

	m.registerCleanupFn(func() error {
		return m.nativeApp.Close()
	})

	return m.nativeApp, nil
}

func setupTraceWriter(svrCtx *server.Context) (traceWriter io.WriteCloser, cleanup func(), err error) {
	// clean up the traceWriter when the server is shutting down
	cleanup = func() {}

	traceWriterFile := svrCtx.Viper.GetString(flagTraceStore)
	traceWriter, err = openTraceWriter(traceWriterFile)
	if err != nil {
		return traceWriter, cleanup, err
	}

	// if flagTraceStore is not used then traceWriter is nil
	if traceWriter != nil {
		cleanup = func() {
			if err = traceWriter.Close(); err != nil {
				svrCtx.Logger.Error("failed to close trace writer", "err", err)
			}
		}
	}

	return traceWriter, cleanup, nil
}

func openDB(rootDir string, backendType db.BackendType) (db.DB, error) {
	dataDir := filepath.Join(rootDir, "data")
	return db.NewDB("application", backendType, dataDir)
}

func openTraceWriter(traceWriterFile string) (w io.WriteCloser, err error) {
	if traceWriterFile == "" {
		return
	}
	return os.OpenFile(
		traceWriterFile,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE,
		0o666,
	)
}

// getApp gets the appropriate app based on the latest application version.
func (m *Multiplexer) getApp() (servertypes.ABCI, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Debug("getting app", "app_version", m.appVersion, "next_app_version", m.nextAppVersion)

	// get the appropriate version for the latest app version.
	currentVersion, err := m.versions.GetForAppVersion(m.appVersion)
	if err != nil {
		// if we are switching from an embedded binary to a native one, we need to ensure that we stop it
		// before we start the native app.
		if err := m.stopEmbeddedApp(); err != nil {
			return nil, fmt.Errorf("failed to stop embedded app: %w", err)
		}

		if m.nativeApp == nil {
			m.logger.Info("no app found in multiplexer for app version, starting latest app", "app_version", m.appVersion)
			app, err := m.startNativeApp()
			if err != nil {
				return nil, fmt.Errorf("failed to start latest app: %w", err)
			}

			// NOTE: we don't need to create a comet node as that will have been created when Start was called.

			if err := m.enableGRPCAndAPIServers(app); err != nil {
				return nil, fmt.Errorf("failed to enable gRPC and API servers: %w", err)
			}
		}

		m.logger.Info("using latest app", "app_version", m.appVersion)
		return m.nativeApp, nil
	}

	// check if we need to start the app or if we have a different app running
	if !m.started || currentVersion.AppVersion > m.activeVersion.AppVersion {
		if err := m.startEmbeddedApp(currentVersion); err != nil {
			return nil, fmt.Errorf("failed to start embedded app: %w", err)
		}
	}

	m.logger.Info("Using ABCI remote connection", "maximum_app_version", m.activeVersion.AppVersion, "abci_version", m.activeVersion.ABCIVersion.String(), "chain_id", m.chainID)

	switch m.activeVersion.ABCIVersion {
	case ABCIClientVersion1:
		return NewRemoteABCIClientV1(m.conn, m.chainID), nil
	case ABCIClientVersion2:
		return NewRemoteABCIClientV2(m.conn), nil
	}

	return nil, fmt.Errorf("unknown ABCI client version %d", m.activeVersion.ABCIVersion)
}

// startEmbeddedApp starts an embedded version of the app.
func (m *Multiplexer) startEmbeddedApp(version Version) error {
	m.logger.Info("starting embedded app", "app_version", version.AppVersion, "abci_version", version.ABCIVersion.String())
	if version.Appd == nil {
		return fmt.Errorf("appd is nil for version %d", m.activeVersion.AppVersion)
	}

	// stop the existing app version if one is currently running.
	if err := m.stopEmbeddedApp(); err != nil {
		return fmt.Errorf("failed to stop active version: %w", err)
	}

	if version.Appd.Pid() == appd.AppdStopped {
		for _, preHandler := range version.PreHandlers {
			preCmd := version.Appd.CreateExecCommand(preHandler)
			if err := preCmd.Run(); err != nil {
				m.logger.Warn("PreHandler failed, continuing without successful PreHandler", "err", err)
				// Continue anyway as the pre-handler might be optional
			}
		}

		// start the new app
		programArgs := os.Args
		if len(os.Args) > 2 {
			programArgs = os.Args[2:] // Remove 'appd start' args
		}

		m.logger.Info("Starting app for version", "app_version", version.AppVersion, "args", version.GetStartArgs(programArgs))
		if err := version.Appd.Start(version.GetStartArgs(programArgs)...); err != nil {
			return fmt.Errorf("failed to start app for version %d: %w", m.appVersion, err)
		}

		if version.Appd.Pid() == appd.AppdStopped {
			return fmt.Errorf("app for version %d failed to start", m.nativeApp)
		}

		m.activeVersion = version
		m.started = true
	}
	return nil
}

// embeddedVersionRunning returns true if there is an active version specified which is not stopped.
func (m *Multiplexer) embeddedVersionRunning() bool {
	return m.activeVersion.Appd != nil && m.activeVersion.Appd.Pid() != appd.AppdStopped
}

// stopEmbeddedApp stops any embedded app versions if they are currently running.
func (m *Multiplexer) stopEmbeddedApp() error {
	if m.embeddedVersionRunning() {
		m.logger.Info("stopping app for version", "active_app_version", m.activeVersion.AppVersion)
		if err := m.activeVersion.Appd.Stop(); err != nil {
			return fmt.Errorf("failed to stop app for version %d: %w", m.activeVersion.AppVersion, err)
		}
		m.started = false
		m.activeVersion = Version{}
	}
	return nil
}

// Cleanup allows proper multiplexer termination.
func (m *Multiplexer) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("cleaning up multiplexer")

	var errs error

	// stop any running app
	if err := m.stopEmbeddedApp(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to stop active version: %w", err))
	}

	// close gRPC connection
	if m.conn != nil {
		if err := m.conn.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("failed to close gRPC connection: %w", err))
		}
		m.conn = nil
	}

	for _, fn := range m.cleanupFns {
		if err := fn(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("failed to run cleanup function: %w", err))
		}
	}

	return errs
}

// startCmtNode initializes and starts a CometBFT node, sets up cleanup tasks, and assigns it to the Multiplexer instance.
func (m *Multiplexer) startCmtNode() error {
	cfg := m.svrCtx.Config
	nodeKey, err := p2p.LoadOrGenNodeKey(cfg.NodeKeyFile())
	if err != nil {
		return err
	}

	// no latest app set means an embedded app is being used.
	if m.nativeApp == nil {
		m.logger.Debug("using embedded app so registering remote app cleanup")
		m.setupRemoteAppCleanup(m.Cleanup)
	}

	tmNode, err := node.NewNodeWithContext(
		m.ctx,
		cfg,
		pvm.LoadOrGenFilePV(cfg.PrivValidatorKeyFile(), cfg.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewConnSyncLocalClientCreator(m),
		internal.GetGenDocProvider(cfg),
		cmtcfg.DefaultDBProvider,
		node.DefaultMetricsProvider(cfg.Instrumentation),
		servercmtlog.CometLoggerWrapper{Logger: m.logger},
	)

	if err != nil {
		return err
	}

	if err := tmNode.Start(); err != nil {
		return err
	}

	m.registerCleanupFn(func() error {
		if tmNode != nil && tmNode.IsRunning() {
			return tmNode.Stop()
		}
		return nil
	})

	m.cmNode = tmNode
	return nil
}

// setupRemoteAppCleanup ensures that remote app processes are terminated when the main process receives termination signals
func (m *Multiplexer) setupRemoteAppCleanup(cleanupFn func() error) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		m.logger.Info("Received signal, stopping remote apps...", "signal", sig)

		if err := cleanupFn(); err != nil {
			m.logger.Error("Error stopping remote apps", "err", err)
		} else {
			m.logger.Info("Successfully stopped remote apps")
		}

		// Re-send the signal to allow the normal process termination
		signal.Reset(os.Interrupt, syscall.SIGTERM)
		syscall.Kill(os.Getpid(), sig.(syscall.Signal))
	}()
}

func startTelemetry(cfg serverconfig.Config) (*telemetry.Metrics, error) {
	return telemetry.New(cfg.Telemetry)
}

// emitServerInfoMetrics emits server info related metrics using application telemetry.
func emitServerInfoMetrics() {
	var ls []metrics.Label

	versionInfo := version.NewInfo()
	if len(versionInfo.GoVersion) > 0 {
		ls = append(ls, telemetry.NewLabel("go", versionInfo.GoVersion))
	}
	if len(versionInfo.CosmosSdkVersion) > 0 {
		ls = append(ls, telemetry.NewLabel("version", versionInfo.CosmosSdkVersion))
	}

	if len(ls) == 0 {
		return
	}

	telemetry.SetGaugeWithLabels([]string{"server", "info"}, 1, ls)
}

func getCtx(svrCtx *server.Context, block bool) (*errgroup.Group, context.Context) {
	ctx, cancelFn := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)
	// listen for quit signals so the calling parent process can gracefully exit
	server.ListenForQuitSignals(g, block, cancelFn, svrCtx.Logger)
	return g, ctx
}
