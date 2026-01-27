package fibre

import (
	"context"
	"fmt"
	"log/slog"

	fibregrpc "github.com/celestiaorg/celestia-app-fibre/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	"github.com/cometbft/cometbft/crypto"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/gogoproto/grpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// ServerConfig contains configuration options for the Fibre [Server].
type ServerConfig struct {
	// ChainID is the chain identifier for domain separation in [PaymentPromise] validation.
	ChainID string

	BlobConfig
	StoreConfig

	// LivenessThreshold is the fraction of stake needed for reconstruction (typically 1/3).
	LivenessThreshold cmtmath.Fraction
	// MinRowsPerValidator is the minimum number of rows each validator must receive
	// for unique decodability security.
	MinRowsPerValidator int
	// MaxMessageSize is the maximum gRPC message size for upload requests.
	MaxMessageSize int

	// Log is the logger for the server.
	// If nil, slog.Default() will be used.
	Log *slog.Logger
	// Tracer is the OpenTelemetry tracer for distributed tracing.
	// If nil, otel.Tracer("fibre-server") will be used.
	Tracer trace.Tracer
}

// DefaultServerConfig returns a [ServerConfig] with default values.
func DefaultServerConfig() ServerConfig {
	return NewServerConfigFromParams(DefaultProtocolParams)
}

// NewServerConfigFromParams creates a ServerConfig with values derived from the given ProtocolParams.
// Use this when you need a config with non-default protocol parameters (e.g., for testing).
func NewServerConfigFromParams(p ProtocolParams) ServerConfig {
	return ServerConfig{
		ChainID:             "celestia",
		BlobConfig:          DefaultBlobConfigV0(), // currently hardcode support for version zero only
		StoreConfig:         DefaultStoreConfig(),
		LivenessThreshold:   p.LivenessThreshold,
		MinRowsPerValidator: p.MinRowsPerValidator(),
		MaxMessageSize:      p.MaxMessageSize(),
	}
}

// Server implements the Fibre gRPC service for validators.
// It handles upload and download requests from clients.
type Server struct {
	types.UnimplementedFibreServer

	cfg ServerConfig

	privVal core.PrivValidator
	pubKey  crypto.PubKey // cached public key from privVal

	queryClient types.QueryClient
	valGet      validator.SetGetter
	store       *Store

	log    *slog.Logger
	tracer trace.Tracer

	cancel context.CancelFunc
}

// NewServer creates a new Fibre [Server] with default Badger store backend.
// Returns an error if the validator's public key cannot be retrieved or
// if Badger instance cannot be started.
func NewServer(
	privVal core.PrivValidator,
	queryClient types.QueryClient,
	valGet validator.SetGetter,
	cfg ServerConfig,
) (*Server, error) {
	store, err := NewBadgerStore(cfg.StoreConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Fibre store: %w", err)
	}

	return newServer(
		privVal,
		queryClient,
		valGet,
		store,
		cfg,
	)
}

// NewServerFromGRPC creates a new Fibre [Server] from a gRPC server and client.
// It registers the server with the gRPC server and returns the Fibre [Server].
func NewServerFromGRPC(
	privVal core.PrivValidator,
	grpcServer grpc.Server,
	grpcClient grpc.ClientConn,
	cfg ServerConfig,
) (*Server, error) {
	queryClient := types.NewQueryClient(grpcClient)
	valGet := fibregrpc.NewSetGetter(coregrpc.NewBlockAPIClient(grpcClient))

	server, err := NewServer(privVal, queryClient, valGet, cfg)
	if err != nil {
		return nil, err
	}
	types.RegisterFibreServer(grpcServer, server)
	return server, nil
}

// NewInMemoryServer creates a new Fibre [Server] with an in-memory store backend.
func NewInMemoryServer(
	privVal core.PrivValidator,
	queryClient types.QueryClient,
	valGet validator.SetGetter,
	cfg ServerConfig,
) (*Server, error) {
	memStore := NewMemoryStore(cfg.StoreConfig)
	return newServer(
		privVal,
		queryClient,
		valGet,
		memStore,
		cfg,
	)
}

func newServer(
	privVal core.PrivValidator,
	queryClient types.QueryClient,
	valGet validator.SetGetter,
	store *Store,
	cfg ServerConfig,
) (*Server, error) {
	if cfg.Log == nil {
		cfg.Log = slog.Default().WithGroup("fibre-server")
	}
	if cfg.Tracer == nil {
		cfg.Tracer = otel.Tracer("fibre-server")
	}

	// cache the validator's public key in case the implementation does IO internally
	pubKey, err := privVal.GetPubKey()
	if err != nil {
		return nil, fmt.Errorf("getting validator public key: %w", err)
	}

	server := &Server{
		cfg:         cfg,
		privVal:     privVal,
		pubKey:      pubKey,
		queryClient: queryClient,
		valGet:      valGet,
		store:       store,
		log:         cfg.Log,
		tracer:      cfg.Tracer,
	}

	return server, nil
}

func (s *Server) Config() ServerConfig {
	return s.cfg
}

// Store returns the server's store.
func (s *Server) Store() *Store {
	return s.store
}

// Start starts background routines for the server.
// It should be called after the server is created. Use [Stop] to stop the background routines.
func (s *Server) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.startPruneLoop(ctx)
}

// Stop stops the server and its background routines.
// NOTE: It is not a graceful shutdown as it doesn't await for pending requests to complete.
func (s *Server) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	return s.store.Close()
}
