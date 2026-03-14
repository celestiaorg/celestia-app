package fibre

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	fibregrpc "github.com/celestiaorg/celestia-app/v8/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v8/fibre/state"
	core "github.com/cometbft/cometbft/types"
	"go.opentelemetry.io/otel/trace"
	grpclib "google.golang.org/grpc"
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
	Metrics *ServerMetrics

	pruneDone chan struct{}
	cancel    context.CancelFunc
}

// NewServer creates a new Fibre [Server]. The store backend is determined by
// [ServerConfig.StoreFn], which defaults to [NewPebbleStore].
func NewServer(cfg ServerConfig) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	stateClient, err := cfg.StateClientFn()
	if err != nil {
		return nil, err
	}

	server := &Server{
		Config:  cfg,
		state:   stateClient,
		log:     cfg.Log,
		tracer:  cfg.Tracer,
		Metrics: cfg.Metrics,
	}

	server.grpc, err = fibregrpc.NewServer(
		cfg.ServerListenAddress,
		server,
		grpclib.MaxRecvMsgSize(cfg.MaxMessageSize),
		grpclib.MaxSendMsgSize(cfg.MaxMessageSize),
	)
	if err != nil {
		return nil, fmt.Errorf("creating gRPC server: %w", err)
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

	s.store, err = s.Config.StoreFn(s.Config.StoreConfig)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}

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
