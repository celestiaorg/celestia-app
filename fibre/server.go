package fibre

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"

	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/fibre/internal/tlsid"
	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d"
	core "github.com/cometbft/cometbft/types"
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

	occ *occupancy

	pruneDone chan struct{}
	cancel    context.CancelFunc
}

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
	}

	server.grpc, err = fibregrpc.Listen(cfg.ServerListenAddress)
	if err != nil {
		return nil, fmt.Errorf("opening gRPC listener: %w", err)
	}

	return server, nil
}

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

// Start connects to the celestia-app node, creates the signer,
// starts serving gRPC requests, and kicks off background pruning.
// NOTE: Order of operations is important. Start the state client first,
// then create the signer, and finally start the pruning loop followed by the gRPC server.
func (s *Server) Start(ctx context.Context) (err error) {
	if err := s.state.Start(ctx); err != nil {
		return err
	}

	s.signer, err = s.Config.SignerFn(s.state.ChainID())
	if err != nil {
		return fmt.Errorf("creating signer: %w", err)
	}
	s.log.Info("signer ready")

	cert, err := tlsid.BuildServerCert(s.signer, s.state.ChainID())
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
		grpclib.Creds(creds),
	)

	s.store, err = s.Config.StoreFn(s.Config.StoreConfig)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}

	size, err := s.store.Size()
	if err != nil {
		return fmt.Errorf("getting store size: %w", err)
	}
	s.occ.seed(size)

	s.recomputeBudget(ctx)

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	s.pruneDone = make(chan struct{})
	go func() {
		defer close(s.pruneDone)
		s.startPruneLoop(ctx)
	}()

	s.grpc.Serve()
	s.log.Info("serving gRPC", "addr", s.grpc.ListenAddress())
	return nil
}

// Stop gracefully stops the gRPC server and background routines,
// then closes the underlying store and app connection.
// Cancelling the context forces an immediate stop without waiting for in-flight requests.
func (s *Server) Stop(ctx context.Context) (err error) {
	s.log.Info("stopping server")
	if s.cancel != nil {
		s.cancel()
	}
	s.grpc.Stop(ctx)
	if s.pruneDone != nil {
		<-s.pruneDone
	}

	if closer, ok := s.signer.(io.Closer); ok {
		if closeErr := closer.Close(); closeErr != nil {
			s.log.Error("closing signer", "error", closeErr)
			err = errors.Join(err, closeErr)
		}
	}
	if s.store != nil {
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
// OriginalRows) and applies it to the counter.
func (s *Server) recomputeBudget(ctx context.Context) {
	if s.Config.FullStakeStorageBudgetFn == nil {
		return
	}

	fullStake, err := s.Config.FullStakeStorageBudgetFn(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "budget recompute failed; keeping previous budget", "error", err)
		return
	}

	valSet, err := s.state.Head(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "budget recompute failed; keeping previous budget", "error", err)
		return
	}

	key, err := s.signer.GetPubKey()
	if err != nil {
		s.log.WarnContext(ctx, "budget recompute failed; keeping previous budget", "error", err)
		return
	}

	ourVal, found := valSet.GetByAddress(key.Address())
	if !found {
		// Not in the active set: no new assignments, but promises for past
		// heights we were assigned to can still arrive, so keep the previous
		// budget rather than dropping to an unlimited (budget == 0) state.
		return
	}

	assignedRows := valSet.AssignedRows(ourVal, s.Config.OriginalRows, s.Config.MinRowsPerValidator, s.Config.LivenessThreshold)
	budget := fullStake * int64(assignedRows) / int64(s.Config.OriginalRows)

	if fullStake > 0 && fullStake < int64(s.Config.MaxShardSize) {
		s.log.ErrorContext(ctx, "FullStakeStorageBudget is below one shard; the limiter will reject every upload",
			"full_stake_budget", fullStake, "max_shard_size", s.Config.MaxShardSize)
	}
	if budget > 0 && s.store != nil {
		if avail, err := s.store.DiskAvailable(); err == nil && avail < budget {
			s.log.WarnContext(ctx, "available disk is below the storage budget; provision more or the disk will fill",
				"available_bytes", avail, "budget_bytes", budget)
		}
	}

	// apply even a flagged budget. A too-small one rejects every upload
	// which is safer than leaving a fresh node at the unlimited budget==0
	// default.
	s.occ.setBudget(budget)
}
