// Package v3 provides a unified async transaction pipeline for Celestia.
//
// TxClientV3 embeds v2.TxClient (which embeds v1) and adds a non-blocking
// AddTx / AddPayForBlob API. All transaction types — regular sdk.Msg and
// MsgPayForBlobs — flow through the same single-goroutine pipeline:
// sign → submit → confirm, with 3-phase callbacks via TxHandle.
//
// IMPORTANT: Once using AddTx/AddPayForBlob, all txs for the same account
// must go through v3's async pipeline. Mixing v1/v2 sync methods on the
// same account would cause sequence conflicts.
package v3

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	"github.com/celestiaorg/celestia-app/v9/pkg/user/v2"
	blobtypes "github.com/celestiaorg/celestia-app/v9/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
)

const defaultQueueSize = 100

// errClientClosed is returned by enqueue and resolved on any handle that
// was still queued when Close was called.
var errClientClosed = errors.New("tx client closed")

// V3Option configures the TxClientV3.
type V3Option func(*TxClientV3)

// WithQueueSize sets the capacity of the async request channel.
func WithQueueSize(size int) V3Option {
	return func(c *TxClientV3) {
		c.queueSize = size
	}
}

// TxClientV3 wraps v2.TxClient and adds a unified async pipeline for all
// transaction types.
type TxClientV3 struct {
	*v2.TxClient
	requestCh chan *TxRequest
	cancel    context.CancelFunc
	done      chan struct{}
	closed    atomic.Bool
	queueSize int
}

// NewTxClientV3 creates a new TxClientV3 wrapping the provided v2 client.
// The async pipeline starts immediately and runs until Close is called or
// the context is cancelled.
func NewTxClientV3(ctx context.Context, v2Client *v2.TxClient, opts ...V3Option) (*TxClientV3, error) {
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

	c := &TxClientV3{
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
		signer:      newSDKTxSigner(v1, accountName),
		broadcaster: newGRPCTxBroadcaster(v1, conns[0]),
		buffer:      newTxBuffer(acc.Sequence()),
		requestCh:   c.requestCh,
		events:      make(chan event, 8),
		pollTime:    v1.PollTime(),
	}

	go func() {
		defer close(c.done)
		w.run(ctx)
	}()

	return c, nil
}

// SetupTxClientV3 creates a fully initialized TxClientV3 by querying the
// chain for account info. This is a convenience constructor equivalent to
// v2.SetupTxClient followed by NewTxClientV3.
func SetupTxClientV3(
	ctx context.Context,
	keys keyring.Keyring,
	conn *grpc.ClientConn,
	encCfg encoding.Config,
	v1Options []user.Option,
	v3Options ...V3Option,
) (*TxClientV3, error) {
	v2Client, err := v2.SetupTxClient(ctx, keys, conn, encCfg, v1Options...)
	if err != nil {
		return nil, err
	}

	return NewTxClientV3(ctx, v2Client, v3Options...)
}

// AddTx submits a transaction to the async pipeline. Non-blocking: returns
// an error only if the queue is full or the client is closed.
func (c *TxClientV3) AddTx(ctx context.Context, msgs []sdktypes.Msg, opts ...user.TxOption) (*TxHandle, error) {
	req, handle := newTxHandle(ctx, msgs, nil, opts)
	return handle, c.enqueue(req)
}

// AddPayForBlob wraps blobs into MsgPayForBlobs and submits via the same
// async pipeline as AddTx. Gas estimation is handled by the signer.
func (c *TxClientV3) AddPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...user.TxOption) (*TxHandle, error) {
	if len(blobs) == 0 {
		return nil, errors.New("at least one blob is required")
	}

	msg, err := blobtypes.NewMsgPayForBlobs("", 0, blobs...)
	if err != nil {
		return nil, fmt.Errorf("creating MsgPayForBlobs: %w", err)
	}

	req, handle := newTxHandle(ctx, []sdktypes.Msg{msg}, blobs, opts)
	return handle, c.enqueue(req)
}

// Close stops the async pipeline, waits for the worker to finish, and
// resolves any requests left in the queue with errClientClosed so callers
// blocked on Await don't hang. Safe to call more than once.
func (c *TxClientV3) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.cancel()
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
func (c *TxClientV3) enqueue(req *TxRequest) error {
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
