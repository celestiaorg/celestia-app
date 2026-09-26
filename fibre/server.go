package fibre

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/bits"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/fibre/internal/tlsid"
	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Server implements the Fibre gRPC service for validators.
// It handles upload and download requests from clients.
type Server struct {
	Config ServerConfig

	state  state.Client
	store  *Store
	grpc   *fibregrpc.Server
	signer core.PrivValidator

	log     *slog.Logger
	tracer  trace.Tracer
	metrics *serverMetrics

	verifiers chan *rsema1d.Verifier // caps concurrent verifications

	occ     *occupancy
	uploads uploadCoordinator

	health        *healthManager
	healthMetrics metric.Registration
	healthLn      net.Listener // optional HTTP health listener
	healthHTTP    *http.Server

	pruneDone chan struct{}
	cancel    context.CancelFunc
	lifecycle sync.Mutex // serializes Start and Stop so Stop cannot interleave a running Start
	started   atomic.Bool
	once      sync.Once
	stopErr   error
}

// shutdownTimeout bounds graceful shutdown; remaining requests are cut off when it elapses.
const shutdownTimeout = 30 * time.Second

// NewServer creates a new Fibre [Server]. The store backend is determined by
// [ServerConfig.StoreFn], which defaults to [NewStore].
func NewServer(cfg ServerConfig) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	stateClient, err := cfg.StateClientFn()
	if err != nil {
		return nil, err
	}

	occ := newOccupancy(0)

	metrics, err := newServerMetrics(cfg.Meter, occ)
	if err != nil {
		return nil, fmt.Errorf("creating metrics: %w", err)
	}

	server := &Server{
		Config:    cfg,
		state:     stateClient,
		log:       cfg.Log,
		tracer:    cfg.Tracer,
		metrics:   metrics,
		verifiers: newVerifierPool(cfg.UploadVerifyWorkers),
		occ:       occ,
		health:    newHealthManager(cfg.health, cfg.Log),
	}
	server.healthMetrics, err = metrics.registerHealthObserver(cfg.Meter, server.health.report)
	if err != nil {
		return nil, fmt.Errorf("registering health metrics: %w", err)
	}
	defer func() {
		if err != nil {
			_ = server.healthMetrics.Unregister()
		}
	}()

	server.grpc, err = fibregrpc.Listen(cfg.ServerListenAddress, cfg.MaxConnections, cfg.MaxConcurrentStreams)
	if err != nil {
		return nil, fmt.Errorf("opening gRPC listener: %w", err)
	}
	if cfg.HealthListenAddress != "" {
		if server.healthLn, err = net.Listen("tcp", cfg.HealthListenAddress); err != nil {
			server.grpc.Stop(context.Background())
			return nil, fmt.Errorf("listen on health address %s: %w", cfg.HealthListenAddress, err)
		}
		server.healthHTTP = &http.Server{Handler: server.health.httpHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	}

	return server, nil
}

// HealthListenAddress returns the HTTP health address, or "" when disabled.
func (s *Server) HealthListenAddress() string {
	if s.healthLn == nil {
		return ""
	}
	return s.healthLn.Addr().String()
}

// Done is closed once the gRPC server stopped serving, through Stop or because
// it failed; Err tells the two apart.
func (s *Server) Done() <-chan struct{} { return s.grpc.Done() }

// Err returns the error the gRPC server exited with, or nil.
func (s *Server) Err() error { return s.grpc.Err() }

// ListenAddress returns the actual address the server is listening on.
func (s *Server) ListenAddress() string {
	return s.grpc.ListenAddress()
}

// ChainID returns the chain ID detected from the connected app node.
func (s *Server) ChainID() string {
	return s.state.ChainID()
}

// Store returns the server's store.
func (s *Server) Store() *Store {
	return s.store
}

// Start connects to the celestia-app node, creates the signer and TLS identity,
// starts serving gRPC, opens the store, derives the storage budget and kicks off
// background pruning and health checks. The HTTP health endpoints answer from
// the first moment of Start and the gRPC health service as soon as the TLS
// identity exists; Fibre RPCs return Unavailable until Start returns. On
// failure every acquired resource is released. Stop waits for a running Start
// to return; cancel ctx to abort a blocked Start.
func (s *Server) Start(ctx context.Context) (err error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if !s.started.CompareAndSwap(false, true) {
		return errors.New("server already started")
	}
	select {
	case <-s.grpc.Done():
		return errors.New("server is stopped")
	default:
	}
	if s.healthHTTP != nil {
		go func() { _ = s.healthHTTP.Serve(s.healthLn) }()
		s.log.Info("serving health HTTP", "addr", s.healthLn.Addr())
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, s.stopOnce(context.Background()))
		}
	}()

	if err := s.state.Start(ctx); err != nil {
		return err
	}
	chainID := s.state.ChainID()
	if expected := s.Config.health.expectedChainID; expected != "" && chainID != expected {
		return fmt.Errorf("app node reports chain ID %q but expected_chain_id is %q", chainID, expected)
	}

	s.signer, err = s.Config.SignerFn(chainID)
	if err != nil {
		return fmt.Errorf("creating signer: %w", err)
	}
	s.log.Info("signer ready")

	cert, err := tlsid.BuildServerCert(s.signer, chainID)
	if err != nil {
		return fmt.Errorf("building TLS cert: %w", err)
	}
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})
	s.grpc.Register(s,
		grpclib.MaxRecvMsgSize(s.Config.MaxMessageSize),
		grpclib.MaxSendMsgSize(s.Config.MaxMessageSize),
		// Reject too many rows or proofs before protobuf allocates for them.
		grpclib.ForceServerCodecV2(fibregrpc.NewServerCodec(
			DefaultProtocolParams.MaxRowsPerValidator(),
			DefaultProtocolParams.MerkleProofDepth(),
		)),
		grpclib.Creds(creds),
		grpclib.ChainUnaryInterceptor(s.health.unaryGate),
	)
	s.health.registerGRPC(s.grpc.Registrar())
	// Serve health (and gated Fibre RPCs) before the remaining, potentially slow initialization.
	s.grpc.Serve()
	s.health.set(true, HealthServiceLiveness)
	go func() { // an unexpected Serve exit fails liveness until the CLI stops the process
		<-s.grpc.Done()
		if err := s.grpc.Err(); err != nil {
			s.health.serveFailed(err)
		}
	}()
	s.log.Info("serving gRPC", "addr", s.grpc.ListenAddress())

	pubKey, err := s.signer.GetPubKey()
	if err != nil {
		return fmt.Errorf("getting validator public key: %w", err)
	}
	s.Config.ObjectStorage.ChainID = chainID
	s.Config.ObjectStorage.ValidatorAddress = sdk.ConsAddress(pubKey.Address()).String()
	s.store, err = s.Config.StoreFn(ctx, s.Config.StoreConfig)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	s.store.shards.setMetrics(s.metrics)

	if err := s.seedOccupancy(ctx); err != nil {
		return err
	}

	// Derive the budget once at startup.
	if err := s.recomputeBudget(ctx); err != nil {
		return fmt.Errorf("deriving initial storage budget: %w", err)
	}
	if !s.Config.UnlimitedBudget && s.occ.budgetBytes() <= 0 {
		s.log.Warn("derived storage budget is 0 (validator not in the active set?); " +
			"running without a storage limit until it is re-derived")
	}

	// Probes must bypass the validator set cache; a client without probe support
	// leaves the server permanently not ready.
	stateClient := s.state
	if cc, ok := stateClient.(*state.CachingClient); ok {
		stateClient = cc.Client
	}
	healthClient, _ := stateClient.(state.HealthClient)

	bgCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	s.pruneDone = make(chan struct{})
	go func() {
		defer close(s.pruneDone)
		s.startPruneLoop(bgCtx)
	}()

	s.health.start(bgCtx, healthDeps{
		client: healthClient,
		module: func(ctx context.Context) error { _, err := s.state.FullStakeStorageBudget(ctx); return err },
		signer: s.signer,
		store:  s.store,
	}, chainID)
	return nil
}

func (s *Server) seedOccupancy(ctx context.Context) error {
	size, err := s.store.Size(ctx)
	if err != nil && !errors.Is(err, ErrStoreIntegrity) {
		return fmt.Errorf("getting store size: %w", err)
	}
	if err != nil {
		s.log.Warn("store size may be incorrect due to corrupt shard marker", "error", err)
	}
	s.occ.seed(size)
	return nil
}

// Stop publishes not-ready, stops background routines, drains the gRPC server
// within shutdownTimeout, then closes the signer, store and app connection.
// Cancelling the context forces an immediate stop. Stop is safe before Start,
// after a failed Start and when repeated.
func (s *Server) Stop(ctx context.Context) error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	return s.stopOnce(ctx)
}

func (s *Server) stopOnce(ctx context.Context) error {
	s.once.Do(func() { s.stopErr = s.stop(ctx) })
	return s.stopErr
}

func (s *Server) stop(ctx context.Context) (err error) {
	s.log.Info("stopping server")
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	if s.cancel != nil {
		s.cancel()
	}
	probesDrained := s.health.stop(ctx)
	if s.healthHTTP != nil {
		err = errors.Join(err, s.healthHTTP.Shutdown(ctx))
	} else if s.healthLn != nil {
		_ = s.healthLn.Close()
	}
	s.grpc.Stop(ctx)
	if s.pruneDone != nil {
		<-s.pruneDone
	}
	_ = s.healthMetrics.Unregister()

	if closer, ok := s.signer.(io.Closer); ok {
		if closeErr := closer.Close(); closeErr != nil {
			s.log.Error("closing signer", "error", closeErr)
			err = errors.Join(err, closeErr)
		}
	}
	switch {
	case s.store == nil:
	case !probesDrained: // closing Pebble under an in-flight probe panics; leave it to process exit
		err = errors.Join(err, errors.New("store left open: a health probe was still running at the shutdown deadline"))
	default:
		if closeErr := s.store.Close(); closeErr != nil {
			s.log.Error("closing store", "error", closeErr)
			err = errors.Join(err, closeErr)
		}
	}
	if s.state != nil {
		if closeErr := s.state.Stop(ctx); closeErr != nil {
			s.log.Error("closing state client", "error", closeErr)
			err = errors.Join(err, closeErr)
		}
	}
	return err
}

// recomputeBudget derives this node's occupancy budget from the governance
// parameter and its current stake (FullStakeStorageBudget * assignedRows /
// OriginalRows) and applies it to the counter. It applies an unlimited (zero)
// budget when the limiter is disabled — via UnlimitedBudget or a non-positive
// governance parameter — and returns an error when the parameter, validator
// set, or signer cannot be read.
func (s *Server) recomputeBudget(ctx context.Context) error {
	if s.Config.UnlimitedBudget {
		s.occ.setBudget(0)
		return nil
	}

	fullStake, err := s.state.FullStakeStorageBudget(ctx)
	if err != nil {
		return fmt.Errorf("querying full-stake storage budget: %w", err)
	}

	// FullStakeStorageBudget must be positive when the limiter is enabled. Treat a
	// non-positive value as an error so we keep the previous budget (or fail
	// startup) rather than silently running unlimited.
	if fullStake <= 0 {
		return fmt.Errorf("full-stake storage budget must be positive (got %d); pass --unlimited-budget to disable the limiter", fullStake)
	}

	valSet, err := s.state.Head(ctx)
	if err != nil {
		return fmt.Errorf("fetching head validator set: %w", err)
	}

	key, err := s.signer.GetPubKey()
	if err != nil {
		return fmt.Errorf("getting validator public key: %w", err)
	}

	ourVal, found := valSet.GetByAddress(key.Address())
	if !found {
		// Not in the active set: no new assignments. Keep whatever budget is
		// already applied. During a periodic recompute that preserves the last
		// derived budget, so promises from heights we were assigned to are still
		// bounded; at startup nothing has been derived yet, so the budget stays 0
		// and Start refuses to boot a node that cannot derive its budget.
		return nil
	}

	assignedRows := valSet.AssignedRows(ourVal, s.Config.OriginalRows, s.Config.MinRowsPerValidator, s.Config.LivenessThreshold)
	budget := deriveBudget(fullStake, assignedRows, s.Config.OriginalRows)

	if fullStake < int64(s.Config.MaxShardSize) {
		s.log.WarnContext(ctx, "FullStakeStorageBudget is below one maximum shard; uploads near the maximum blob size will be rejected",
			"full_stake_budget", fullStake, "max_shard_size", s.Config.MaxShardSize)
	}
	if budget > 0 {
		if avail, err := s.store.DiskAvailable(); err == nil && avail < budget {
			s.log.WarnContext(ctx, "available disk is below the storage budget; provision more or the disk will fill",
				"available_bytes", avail, "budget_bytes", budget)
		}
	}

	s.occ.setBudget(budget)
	return nil
}

// deriveBudget returns fullStake * assignedRows / originalRows in bytes. The
// product can exceed int64 for a large fullStake, so it is computed in 128 bits;
// with assignedRows <= originalRows the result stays within [0, fullStake].
// Non-positive inputs yield 0, which disables the limiter.
func deriveBudget(fullStake int64, assignedRows, originalRows int) int64 {
	if fullStake <= 0 || assignedRows <= 0 || originalRows <= 0 {
		return 0
	}
	hi, lo := bits.Mul64(uint64(fullStake), uint64(assignedRows))
	quo, _ := bits.Div64(hi, lo, uint64(originalRows))
	return int64(quo)
}
