package v3

import (
	"context"

	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	"github.com/celestiaorg/go-square/v4/share"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
)

// TxHandle provides 3-phase callbacks for tracking an async transaction
// through the pipeline: signed, submitted, confirmed. Each channel receives
// exactly one value and then closes.
type TxHandle struct {
	// Signed is sent when the transaction has been signed and a sequence assigned.
	Signed <-chan SignedResult
	// Submitted is sent when the transaction has been accepted by at least one node.
	Submitted <-chan SubmittedResult
	// Confirmed is terminal: the transaction was committed, rejected, or errored.
	Confirmed <-chan ConfirmedResult
}

// SignedResult contains information about a successfully signed transaction.
type SignedResult struct {
	TxHash   string
	Sequence uint64
}

// SubmittedResult contains information about a successfully submitted transaction.
type SubmittedResult struct {
	TxHash string
}

// ConfirmedResult contains the terminal result of a transaction.
type ConfirmedResult struct {
	Response *sdktypes.TxResponse
	Err      error
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

	// Callback channels (send side). The worker sends exactly one value on each
	// and then closes the channel.
	signedCh    chan<- SignedResult
	submittedCh chan<- SubmittedResult
	confirmedCh chan<- ConfirmedResult
}

// newTxHandle creates a TxRequest with its associated TxHandle.
// The TxHandle is returned to the caller; the TxRequest is sent to the worker.
func newTxHandle(ctx context.Context, msgs []sdktypes.Msg, blobs []*share.Blob, opts []user.TxOption) (*TxRequest, *TxHandle) {
	signedCh := make(chan SignedResult, 1)
	submittedCh := make(chan SubmittedResult, 1)
	confirmedCh := make(chan ConfirmedResult, 1)

	req := &TxRequest{
		Msgs:        msgs,
		Blobs:       blobs,
		Opts:        opts,
		Ctx:         ctx,
		signedCh:    signedCh,
		submittedCh: submittedCh,
		confirmedCh: confirmedCh,
	}

	handle := &TxHandle{
		Signed:    signedCh,
		Submitted: submittedCh,
		Confirmed: confirmedCh,
	}

	return req, handle
}
