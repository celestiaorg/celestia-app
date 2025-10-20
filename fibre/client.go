package fibre

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	clock "github.com/filecoin-project/go-clock"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// DefaultKeyName is the default key name for the client.
// Exposed for testing purposes.
const DefaultKeyName = "default-fibre"

var (
	// ErrClientClosed is returned when an operation is attempted on a closed client.
	ErrClientClosed = errors.New("fibre client is closed")
	// ErrKeyNotFound is returned when the configured key is not found in the keyring.
	ErrKeyNotFound = errors.New("key not found in keyring")
)

// ClientConfig contains configuration options for the Fibre [Client].
type ClientConfig struct {
	// DefaultKeyName is the name of the key in the keyring to use for signing [PaymentPromise]s.
	DefaultKeyName string
	// ChainID is the chain identifier for domain separation in [PaymentPromise] signatures.
	ChainID string

	// BlobConfig contains erasure coding and data handling configuration.
	BlobConfig

	// UploadTargetVotingPower is the fraction (e.g., 2/3) of total voting power required for Upload operations.
	UploadTargetVotingPower cmtmath.Fraction
	// UploadTargetSignaturesCount is the fraction (e.g., 2/3) of total signature count required for Upload operations.
	UploadTargetSignaturesCount cmtmath.Fraction
	// UploadConcurrency is the maximum number of concurrent uploads to validators.
	UploadConcurrency int
	// DownloadConcurrency is the maximum number of concurrent read requests to validators.
	DownloadConcurrency int

	// NewClientFn is the constructor function for creating [types.Client]s.
	// If nil, [types.DefaultFibreClientFn] will be used.
	NewClientFn grpc.NewClientFn
	// Log is the logger for the client.
	// If nil, [slog.Default] will be used.
	Log *slog.Logger
	// Tracer is the OpenTelemetry tracer for distributed tracing.
	// If nil, [trace.Default] will be used.
	Tracer trace.Tracer
	// Clock is the clock for time-related operations.
	// If nil, [clock.New] will be used.
	Clock clock.Clock
}

// DefaultClientConfig returns a [ClientConfig] with the default values.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		DefaultKeyName:              DefaultKeyName,
		ChainID:                     "celestia",
		BlobConfig:                  DefaultBlobConfig(),
		UploadTargetVotingPower:     cmtmath.Fraction{Numerator: 2, Denominator: 3},
		UploadTargetSignaturesCount: cmtmath.Fraction{Numerator: 2, Denominator: 3},
		UploadConcurrency:           100, // matches expected number of validators to maximize throughput by default
		DownloadConcurrency:         25,  // 1/4 of validators to match 1/3 erasure coding overhead and request the minimum number of samples to get the data
	}
}

// Client is the Fibre DA client.
type Client struct {
	cfg ClientConfig

	txClient *user.TxClient
	keyring  keyring.Keyring
	valGet   validator.SetGetter
	hostReg  validator.HostRegistry

	log    *slog.Logger
	tracer trace.Tracer
	clock  clock.Clock

	clientCache *grpc.ClientCache
	uploadSem   chan struct{}
	downloadSem chan struct{}

	// closeWg tracks subroutines spawned by Upload/Download operations.
	// Close() waits for this WaitGroup to ensure all operations complete before releasing resources.
	// Upload/Download operations don't wait for their spawned goroutines, allowing them to return early for low latency.
	closeWg sync.WaitGroup
	// closed indicates whether Close() has been called.
	closed atomic.Bool
}

// NewClient creates a new [Client] with the provided dependencies.
// Returns an error if the configured key is not found in the keyring.
func NewClient(txClient *user.TxClient, kr keyring.Keyring, valGet validator.SetGetter, hostReg validator.HostRegistry, cfg ClientConfig) (*Client, error) {
	// Verify the key exists in the keyring
	_, err := kr.Key(cfg.DefaultKeyName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrKeyNotFound, cfg.DefaultKeyName, err)
	}

	if cfg.NewClientFn == nil {
		cfg.NewClientFn = grpc.DefaultNewClientFn(hostReg)
	}
	if cfg.Tracer == nil {
		cfg.Tracer = otel.Tracer("fibre")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.New()
	}

	return &Client{
		cfg:         cfg,
		txClient:    txClient,
		keyring:     kr,
		valGet:      valGet,
		hostReg:     hostReg,
		log:         cfg.Log,
		tracer:      cfg.Tracer,
		clock:       cfg.Clock,
		clientCache: grpc.NewClientCache(cfg.NewClientFn, cfg.UploadConcurrency),
		uploadSem:   make(chan struct{}, cfg.UploadConcurrency),
		downloadSem: make(chan struct{}, cfg.DownloadConcurrency),
	}, nil
}

// Config returns the [ClientConfig] used by this client.
func (c *Client) Config() ClientConfig {
	return c.cfg
}

// Close closes the client and releases any associated resources.
// It waits for all ongoing [Client.Upload]/[Client.Download] operations to complete before closing.
// After Close is called, subsequent [Client.Upload/Client.Download] calls will return an error.
// Close is idempotent and safe to call multiple times.
func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	c.closeWg.Wait()
	return c.clientCache.Close()
}
