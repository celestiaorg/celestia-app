package fibre

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	fibregrpc "github.com/celestiaorg/celestia-app/v8/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v8/fibre/state"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	clock "github.com/filecoin-project/go-clock"
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

// Client is the Fibre DA client.
type Client struct {
	Config ClientConfig

	keyring keyring.Keyring
	state   state.Client

	log    *slog.Logger
	tracer trace.Tracer
	clock  clock.Clock

	clientCache *fibregrpc.ClientCache
	uploadSem   chan struct{}
	downloadSem chan struct{}

	// closeWg tracks subroutines spawned by Upload/Download operations.
	// Close() waits for this WaitGroup to ensure all operations complete before releasing resources.
	// Upload/Download operations don't wait for their spawned goroutines, allowing them to return early for low latency.
	closeWg sync.WaitGroup
	// started indicates whether Start() has been called.
	started atomic.Bool
	// closed indicates whether Close() has been called.
	closed atomic.Bool
}

// NewClient creates a new [Client] with the provided dependencies.
// Returns an error if the configured key is not found in the keyring.
func NewClient(kr keyring.Keyring, cfg ClientConfig) (*Client, error) {
	// verify the key exists in the keyring
	_, err := kr.Key(cfg.DefaultKeyName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrKeyNotFound, cfg.DefaultKeyName, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	stateClient, err := cfg.StateClientFn()
	if err != nil {
		return nil, fmt.Errorf("create state client: %w", err)
	}

	if cfg.NewClientFn == nil {
		cfg.NewClientFn = fibregrpc.DefaultNewClientFn(stateClient, cfg.MaxMessageSize)
	}

	return &Client{
		Config:      cfg,
		keyring:     kr,
		state:       stateClient,
		log:         cfg.Log,
		tracer:      cfg.Tracer,
		clock:       cfg.Clock,
		clientCache: fibregrpc.NewClientCache(cfg.NewClientFn, cfg.UploadConcurrency),
		uploadSem:   make(chan struct{}, cfg.UploadConcurrency),
		downloadSem: make(chan struct{}, cfg.DownloadConcurrency),
	}, nil
}

// ChainID returns the chain ID resolved during [Start].
func (c *Client) ChainID() string {
	return c.state.ChainID()
}

// Await waits for all ongoing [Client.Upload]/[Client.Download] operations to complete.
// Await is idempotent and safe to call multiple times.
func (c *Client) Await() {
	c.closeWg.Wait()
}

// Start initializes the client by starting the underlying [StateClient]
// (e.g. auto-detecting the chain ID from the node).
// Must be called before [Client.Upload] or [Client.Download].
func (c *Client) Start(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return nil
	}
	if err := c.state.Start(ctx); err != nil {
		c.started.Store(false)
		return err
	}
	c.log.Info("client ready", "chain_id", c.state.ChainID())
	return nil
}

// Stop stops the client and releases any associated resources.
// It waits for all ongoing [Client.Upload]/[Client.Download] operations to complete before stopping.
// After Stop is called, subsequent [Client.Upload]/[Client.Download] calls will return an error.
// Stop is idempotent and safe to call multiple times.
// Cancelling the context forces an immediate stop without waiting for in-flight operations.
func (c *Client) Stop(ctx context.Context) error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	c.log.Info("stopping client")
	done := make(chan struct{})
	go func() {
		c.closeWg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return c.clientCache.Close()
}
