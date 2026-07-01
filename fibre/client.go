package fibre

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"

	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	clock "github.com/filecoin-project/go-clock"
	"go.opentelemetry.io/otel/trace"
)

// DefaultKeyName is the default key name for the client.
// Exposed for testing purposes.
const DefaultKeyName = "default-fibre"

var (
	// ErrClientClosed is returned when an operation is attempted on a closed client.
	ErrClientClosed = errors.New("fibre: client is closed")
	// ErrKeyNotFound is returned when the configured key is not found in the keyring.
	ErrKeyNotFound = errors.New("fibre: key not found in keyring")
)

// Client is the Fibre DA client.
type Client struct {
	Config ClientConfig

	keyring keyring.Keyring
	state   state.Client

	log     *slog.Logger
	tracer  trace.Tracer
	metrics *clientMetrics
	clock   clock.Clock

	clientCache *fibregrpc.ClientCache

	// escrowLedgers holds one client-side escrow accountant per signer address,
	// created lazily on first use. It guards local reservation and auto-funding
	// so uploads don't fail on an underfunded escrow. See [escrowLedger].
	escrowMu      sync.Mutex
	escrowLedgers map[string]*escrowLedger
	// escrowSeq makes each escrow reservation key unique. The blob ID alone is
	// content-addressed, so two uploads of identical data would otherwise collide
	// on one (idempotent) reservation while settling two on-chain payments.
	escrowSeq atomic.Uint64

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
		cfg.NewClientFn = fibregrpc.DefaultNewClientFn(stateClient, stateClient.ChainID, cfg.MaxMessageSize, cfg.Log)
	}

	metrics, err := newClientMetrics(cfg.Meter)
	if err != nil {
		return nil, fmt.Errorf("creating metrics: %w", err)
	}

	return &Client{
		Config:        cfg,
		keyring:       kr,
		state:         stateClient,
		log:           cfg.Log,
		tracer:        cfg.Tracer,
		metrics:       metrics,
		clock:         cfg.Clock,
		clientCache:   fibregrpc.NewClientCache(cfg.NewClientFn, stateClient, DefaultProtocolParams.MaxValidatorCount, fibregrpc.WithTracer(cfg.Tracer)),
		escrowLedgers: make(map[string]*escrowLedger),
	}, nil
}

// escrowLedgerFor returns the escrow ledger for the account behind txClient,
// creating it (and its deposit/query adapters) on first use. The signer is the
// tx client's default address — the escrow owner that signs PaymentPromises.
func (c *Client) escrowLedgerFor(txClient *user.TxClient) *escrowLedger {
	signer := txClient.DefaultAddress().String()
	c.escrowMu.Lock()
	defer c.escrowMu.Unlock()
	if l, ok := c.escrowLedgers[signer]; ok {
		return l
	}
	l := newEscrowLedger(signer, c.Config.Escrow, c.clock,
		newTxEscrowQuerier(txClient), txDepositor{tx: txClient}, c.log)
	c.escrowLedgers[signer] = l
	return l
}

// newEscrowReservationKey returns a process-unique key identifying one upload's
// escrow reservation. A [BlobID] is content-addressed (identical data yields an
// identical BlobID), so it cannot key the reservation on its own: two uploads of
// the same bytes would share one idempotent reservation while each still settles
// its own on-chain payment (overcommit). A monotonic sequence guarantees
// uniqueness; the BlobID prefix carries no semantics beyond making the key
// legible in logs and traces, and the "#" merely separates the two parts.
func (c *Client) newEscrowReservationKey(blobID BlobID) string {
	return blobID.String() + "#" + strconv.FormatUint(c.escrowSeq.Add(1), 10)
}

// maintainEscrowAsync runs ledger upkeep (refill + reconcile) off the Put hot
// path, tracked by closeWg so Close waits for in-flight deposits. It is a no-op
// once the client is closed.
func (c *Client) maintainEscrowAsync(ledger *escrowLedger) {
	if c.closed.Load() {
		return
	}
	c.closeWg.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*c.Config.RPCTimeout)
		defer cancel()
		ledger.maintain(ctx)
	})
}

// ChainID returns the chain ID resolved during [Start].
func (c *Client) ChainID() string {
	return c.state.ChainID()
}

// validatorSet fetches the validator set bounded by [ClientConfig.RPCTimeout]
// so a hung app node cannot stall an Upload/Download before any shard is
// exchanged. A height of 0 returns the head set; a non-zero height returns the
// set at that height.
func (c *Client) validatorSet(ctx context.Context, height uint64) (validator.Set, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Config.RPCTimeout)
	defer cancel()
	if height > 0 {
		return c.state.GetByHeight(ctx, height)
	}
	return c.state.Head(ctx)
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
