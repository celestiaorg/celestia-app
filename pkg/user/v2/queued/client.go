// Package queued provides a unified async transaction pipeline for Celestia.
//
// Client embeds v2.TxClient (which embeds v1) and adds a non-blocking
// AddTx / AddPayForBlob API. All transaction types — regular sdk.Msg and
// MsgPayForBlobs — flow through the same single-goroutine pipeline:
// sign → submit → confirm, with 3-phase callbacks via TxHandle.
//
// IMPORTANT: Once using AddTx/AddPayForBlob, all txs for the same account
// must go through this async pipeline. Mixing v1/v2 sync methods on the
// same account would cause sequence conflicts.
package queued

import (
	"context"
	"errors"
	"fmt"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	"github.com/celestiaorg/celestia-app/v9/pkg/user/v2"
	blobtypes "github.com/celestiaorg/celestia-app/v9/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
	"sync/atomic"
	"time"
)

const defaultQueueSize = 100

// defaultSubmitRetryDelay is the fixed backoff before re-submitting an entry
// after a transient broadcast error (mempool full / network)
const defaultSubmitRetryDelay = 500 * time.Millisecond

// errClientClosed is returned by enqueue and resolved on any handle that
// was still queued when Close was called.
var errClientClosed = errors.New("tx client closed")

// Option configures the Client.
type Option func(*Client)

// WithQueueSize sets the capacity of the intake channel between AddTx/
// AddPayForBlob and the worker. It is NOT an in-flight or memory bound:
// the worker eagerly drains this channel into an unbounded internal buffer,
// so this only sizes the hand-off and determines when AddTx/AddPayForBlob
// return "tx queue is full" (which, under steady load, is rarely). Callers
// that need to bound outstanding transactions or memory must throttle
// submission themselves. (This matches Lumina's add_tx_capacity.)
func WithQueueSize(size int) Option {
	return func(c *Client) {
		c.queueSize = size
	}
}

// Client wraps v2.TxClient and adds a unified async pipeline for all
// transaction types.
type Client struct {
	*v2.TxClient
	requestCh chan *TxRequest
	cancel    context.CancelFunc
	done      chan struct{}
	closed    atomic.Bool
	queueSize int
}

// NewClient creates a new Client wrapping the provided v2 client.
// The async pipeline starts immediately and runs until Close is called or
// the context is cancelled.
func NewClient(ctx context.Context, v2Client *v2.TxClient, opts ...Option) (*Client, error) {
	if v2Client == nil {
		return nil, errors.New("v2 client must not be nil")
	}

	v1 := v2Client.TxClient
	signer := v1.Signer()
	accountName := v1.DefaultAccountName()

	acc, exists := signer.GetAccount(accountName)
	if !exists {
		return nil, fmt.Errorf("default account %s not found in signer", accountName)
	}

	conns := v1.Conns()
	if len(conns) == 0 {
		return nil, errors.New("v1 client has no gRPC connections")
	}

	c := &Client{
		TxClient:  v2Client,
		queueSize: defaultQueueSize,
	}
	for _, opt := range opts {
		opt(c)
	}

	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	c.requestCh = make(chan *TxRequest, c.queueSize)
	c.done = make(chan struct{})

	w := &worker{
		signer:           newSDKTxSigner(v1, accountName),
		broadcaster:      newGRPCTxBroadcaster(v1, conns[0]),
		buffer:           newTxBuffer(acc.Sequence()),
		requestCh:        c.requestCh,
		events:           make(chan event, 8),
		pollTime:         v1.PollTime(),
		submitRetryDelay: defaultSubmitRetryDelay,
	}

	go func() {
		w.run(ctx)
		close(c.done)
		// Safety net: if the parent ctx was cancelled externally and the
		// user never calls Close, requestCh would otherwise hold orphaned
		// requests forever. The CAS in Close makes the duplicate call a
		// no-op when the user got there first.
		c.Close()
	}()

	return c, nil
}

// SetupClient creates a fully initialized Client by querying the
// chain for account info. This is a convenience constructor equivalent to
// v2.SetupTxClient followed by NewClient.
func SetupClient(
	ctx context.Context,
	keys keyring.Keyring,
	conn *grpc.ClientConn,
	encCfg encoding.Config,
	v1Options []user.Option,
	options ...Option,
) (*Client, error) {
	v2Client, err := v2.SetupTxClient(ctx, keys, conn, encCfg, v1Options...)
	if err != nil {
		return nil, err
	}

	return NewClient(ctx, v2Client, options...)
}

// AddTx submits a transaction to the async pipeline. Non-blocking: returns
// an error only if the queue is full or the client is closed.
func (c *Client) AddTx(ctx context.Context, msgs []sdktypes.Msg, opts ...user.TxOption) (*TxHandle, error) {
	req, handle := newTxHandle(ctx, msgs, nil, opts)
	if err := c.enqueue(req); err != nil {
		// Return a nil handle on failure:
		// the request never entered the pipeline and would never be resolved,
		// so a returned handle could only deadlock a caller that Awaits it.
		return nil, err
	}
	return handle, nil
}

// AddPayForBlob wraps blobs into MsgPayForBlobs and submits via the same
// async pipeline as AddTx. Gas estimation is handled by the signer.
//
// The signer address is resolved here from the embedded TxClient rather
// than asked of the caller: this keeps the API symmetric with AddTx (the
// caller never has to know about accounts) and lets NewMsgPayForBlobs run
// its full validation eagerly, including ValidateBlobShareVersion which
// requires the signer bytes. The worker rebuilds the message during
// signing (see signer.go); this construction is purely for fail-fast.
func (c *Client) AddPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...user.TxOption) (*TxHandle, error) {
	if len(blobs) == 0 {
		return nil, errors.New("at least one blob is required")
	}

	signer := c.Signer()
	acc, exists := signer.GetAccount(c.DefaultAccountName())
	if !exists {
		return nil, fmt.Errorf("default account %s not found in signer", c.DefaultAccountName())
	}

	msg, err := blobtypes.NewMsgPayForBlobs(acc.Address().String(), 0, blobs...)
	if err != nil {
		return nil, fmt.Errorf("creating MsgPayForBlobs: %w", err)
	}

	req, handle := newTxHandle(ctx, []sdktypes.Msg{msg}, blobs, opts)
	if err := c.enqueue(req); err != nil {
		return nil, err // nil handle on failure; see AddTx.
	}
	return handle, nil
}

// Close stops the async pipeline, waits for the worker to finish, and
// resolves any requests left in the queue with errClientClosed so callers
// blocked on Await don't hang. Safe to call more than once.
func (c *Client) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	<-c.done
	for {
		select {
		case req := <-c.requestCh:
			req.resolve(nil, errClientClosed)
		default:
			return
		}
	}
}

// enqueue is non-blocking: returns errClientClosed if Close was called,
// nil on success, or "tx queue is full" if the buffered channel is full.
func (c *Client) enqueue(req *TxRequest) error {
	if c.closed.Load() {
		return errClientClosed
	}
	select {
	case c.requestCh <- req:
		return nil
	default:
		return errors.New("tx queue is full")
	}
}
