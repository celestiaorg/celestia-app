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
	"sync"

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
	wg        sync.WaitGroup
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

	// Get the starting sequence for the buffer.
	acc, exists := signer.GetAccount(accountName)
	if !exists {
		return nil, fmt.Errorf("default account %s not found in signer", accountName)
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

	requestCh := make(chan *TxRequest, c.queueSize)
	c.requestCh = requestCh

	// Use the primary connection for both submission and confirmation.
	conn := v1.Conns()[0]

	w := &worker{
		v1Client:    v1,
		conn:        conn,
		buffer:      newTxBuffer(acc.Sequence()),
		requestCh:   requestCh,
		pollTime:    v1.PollTime(),
		accountName: accountName,
	}

	c.wg.Go(func() {
		w.run(ctx)
	})

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

// AddTx submits a transaction to the async pipeline. It is non-blocking:
// it creates a TxHandle with 3 phase channels and sends the request to
// the worker. Returns an error only if the queue is full or context is cancelled.
func (c *TxClientV3) AddTx(ctx context.Context, msgs []sdktypes.Msg, opts ...user.TxOption) (*TxHandle, error) {
	req, handle := newTxHandle(ctx, msgs, nil, opts)
	return handle, c.enqueue(ctx, req)
}

// AddPayForBlob wraps blobs into MsgPayForBlobs and submits via the same
// async pipeline as AddTx. Gas estimation is handled by the worker.
func (c *TxClientV3) AddPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...user.TxOption) (*TxHandle, error) {
	if len(blobs) == 0 {
		return nil, errors.New("at least one blob is required")
	}

	// Create a placeholder MsgPayForBlobs for the msgs slice.
	// The actual signing uses the blobs directly via CreatePayForBlobs.
	msg, err := blobtypes.NewMsgPayForBlobs("", 0, blobs...)
	if err != nil {
		return nil, fmt.Errorf("creating MsgPayForBlobs: %w", err)
	}

	req, handle := newTxHandle(ctx, []sdktypes.Msg{msg}, blobs, opts)
	return handle, c.enqueue(ctx, req)
}

// Close stops the async pipeline and waits for the worker to finish.
// All pending and in-flight transactions will receive errors.
func (c *TxClientV3) Close() {
	c.cancel()
	c.wg.Wait()
}

// enqueue sends a request to the worker, respecting context cancellation and
// queue backpressure.
func (c *TxClientV3) enqueue(ctx context.Context, req *TxRequest) error {
	select {
	case c.requestCh <- req:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("tx queue is full")
	}
}
