package v3

import (
	"context"

	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	"github.com/celestiaorg/go-square/v4/share"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
)

// TxHandle is returned by AddTx/AddPayForBlob. Call Await to block until the
// transaction reaches a terminal state (committed, rejected, or errored).
type TxHandle struct {
	resp *sdktypes.TxResponse
	err  error

	done chan struct{}
}

// Await blocks until the transaction reaches a terminal state or ctx is
// cancelled. Safe to call multiple times: subsequent calls return the same
// result immediately.
func (h *TxHandle) Await(ctx context.Context) (*sdktypes.TxResponse, error) {
	select {
	case <-h.done:
		return h.resp, h.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TxRequest is the internal representation of a transaction request flowing
// through the async pipeline.
type TxRequest struct {
	// Msgs is always set. For PFB, this contains the MsgPayForBlobs.
	Msgs []sdktypes.Msg
	// Blobs is set only for PFB transactions (needed for CreatePayForBlobs signer path).
	Blobs []*share.Blob
	// Opts are TxOptions for gas, fee, memo, etc.
	Opts []user.TxOption
	// Ctx is the caller's context for this request.
	Ctx context.Context

	// handle is set by the worker when the tx reaches a terminal state.
	handle *TxHandle
}

// resolve sets the terminal result on the handle and unblocks Await.
// Safe to call only once per request.
func (r *TxRequest) resolve(resp *sdktypes.TxResponse, err error) {
	r.handle.resp = resp
	r.handle.err = err
	close(r.handle.done)
}

// newTxHandle creates a TxRequest with its associated TxHandle.
func newTxHandle(ctx context.Context, msgs []sdktypes.Msg, blobs []*share.Blob, opts []user.TxOption) (*TxRequest, *TxHandle) {
	handle := &TxHandle{done: make(chan struct{})}
	req := &TxRequest{
		Msgs:   msgs,
		Blobs:  blobs,
		Opts:   opts,
		Ctx:    ctx,
		handle: handle,
	}
	return req, handle
}
