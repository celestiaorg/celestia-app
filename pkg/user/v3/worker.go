package v3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v9/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v9/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	blobtypes "github.com/celestiaorg/celestia-app/v9/x/blob/types"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/cometbft/cometbft/rpc/core"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
)

const (
	signTickRate   = 50 * time.Millisecond
	submitTickRate = 100 * time.Millisecond
)

// workerMode represents the three modes of the worker loop.
type workerMode int

const (
	// modeSubmitting: normal operation — sign pending, submit next, confirm in parallel.
	modeSubmitting workerMode = iota
	// modeRecovering: submissions paused, confirming one-by-one up to recoverTarget.
	modeRecovering
	// modeStopped: confirm up to stopSeq, then drain remaining with errors and exit.
	modeStopped
)

// worker is the single background goroutine that owns all mutable state
// for the async pipeline. It implements the canonical 3-mode model:
// Submitting → Recovering → Stopped.
//
// Within one run, signed transactions are never re-signed.
// A fatal error (one requiring re-signing) triggers Stop mode.
type worker struct {
	v1Client    *user.TxClient
	conn        *grpc.ClientConn // single connection for submit + confirm
	buffer      *txBuffer
	requestCh   <-chan *TxRequest
	pollTime    time.Duration
	accountName string

	mode          workerMode
	stopSeq       uint64 // Stop mode: confirm entries with seq < stopSeq, then exit
	stopErr       error  // Stop mode: the fatal error that triggered stop
	recoverTarget uint64 // Recovery mode: confirm up to this seq, then resume submitting
}

// run is the main event loop.
func (w *worker) run(ctx context.Context) {
	signTicker := time.NewTicker(signTickRate)
	submitTicker := time.NewTicker(submitTickRate)
	confirmTicker := time.NewTicker(w.pollTime)
	defer signTicker.Stop()
	defer submitTicker.Stop()
	defer confirmTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.drainAll(ctx.Err())
			return

		case req, ok := <-w.requestCh:
			if !ok {
				w.drainAll(fmt.Errorf("request channel closed"))
				return
			}
			w.buffer.addPending(req)

		case <-signTicker.C:
			if w.mode == modeSubmitting {
				w.signPending(ctx)
			}

		case <-submitTicker.C:
			if w.mode == modeSubmitting {
				w.submitNext(ctx)
			}

		case <-confirmTicker.C:
			w.confirmNext(ctx)
			if w.mode == modeStopped && w.allConfirmedUpToStop() {
				w.drainRemaining()
				return
			}
		}
	}
}

// --- Signing ---

// signPending signs all pending requests and appends them to the signed buffer.
func (w *worker) signPending(ctx context.Context) {
	for w.buffer.pendingLen() > 0 {
		req := w.buffer.popPending()
		if req.Ctx.Err() != nil {
			w.sendConfirmed(req, nil, req.Ctx.Err())
			continue
		}

		txBytes, txHash, seq, err := w.signRequest(ctx, req)
		if err != nil {
			w.sendConfirmed(req, nil, fmt.Errorf("signing tx: %w", err))
			continue
		}

		entry := txEntry{
			sequence: seq,
			txHash:   txHash,
			txBytes:  txBytes,
			request:  req,
		}
		if err := w.buffer.appendSigned(entry); err != nil {
			w.sendConfirmed(req, nil, fmt.Errorf("buffer append: %w", err))
			continue
		}
	}
}

// signRequest signs a single request. No re-signing — if it fails, the caller gets an error.
func (w *worker) signRequest(ctx context.Context, req *TxRequest) (txBytes []byte, txHash string, seq uint64, err error) {
	w.v1Client.Lock()
	defer w.v1Client.Unlock()

	if err := w.v1Client.CheckAccountLoaded(ctx, w.accountName); err != nil {
		return nil, "", 0, err
	}

	if req.Blobs != nil {
		return w.signPFB(ctx, req)
	}
	return w.signRegular(ctx, req)
}

// signPFB signs a PayForBlobs transaction.
func (w *worker) signPFB(ctx context.Context, req *TxRequest) ([]byte, string, uint64, error) {
	signer := w.v1Client.Signer()
	acc, exists := signer.GetAccount(w.accountName)
	if !exists {
		return nil, "", 0, fmt.Errorf("account %s not found", w.accountName)
	}

	addr := acc.Address().String()
	msg, err := blobtypes.NewMsgPayForBlobs(addr, 0, req.Blobs...)
	if err != nil {
		return nil, "", 0, err
	}

	gasPrice, gasLimit, err := w.v1Client.EstimateGasPriceAndUsage(ctx, []sdktypes.Msg{msg}, gasestimation.TxPriority_TX_PRIORITY_MEDIUM, req.Opts...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("estimating gas: %w", err)
	}
	fee := uint64(math.Ceil(gasPrice * float64(gasLimit)))
	opts := append([]user.TxOption{user.SetGasLimit(gasLimit), user.SetFee(fee)}, req.Opts...)

	txBytes, seq, err := signer.CreatePayForBlobs(w.accountName, req.Blobs, opts...)
	if err != nil {
		return nil, "", 0, err
	}

	if err := signer.IncrementSequence(w.accountName); err != nil {
		return nil, "", 0, err
	}

	hash := computeTxHash(txBytes)
	return txBytes, hash, seq, nil
}

// signRegular signs a regular (non-PFB) transaction.
// No HandleSequenceMismatch retry — if gas estimation fails, the error is returned.
func (w *worker) signRegular(ctx context.Context, req *TxRequest) ([]byte, string, uint64, error) {
	signer := w.v1Client.Signer()

	txBuilder, err := signer.TxBuilder(req.Msgs, req.Opts...)
	if err != nil {
		return nil, "", 0, err
	}

	hasUserSetFee := false
	for _, coin := range txBuilder.GetTx().GetFee() {
		if coin.Denom == appconsts.BondDenom {
			hasUserSetFee = true
			break
		}
	}

	gasLimit := txBuilder.GetTx().GetGas()
	if gasLimit == 0 {
		if !hasUserSetFee {
			txBuilder.SetFeeAmount(sdktypes.NewCoins(sdktypes.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1))))
		}
		gasLimit, err = w.v1Client.EstimateGasForTx(ctx, txBuilder)
		if err != nil {
			return nil, "", 0, fmt.Errorf("gas estimation: %w", err)
		}
		txBuilder.SetGasLimit(gasLimit)
	}

	if !hasUserSetFee {
		fee := int64(math.Ceil(appconsts.DefaultMinGasPrice * float64(gasLimit)))
		txBuilder.SetFeeAmount(sdktypes.NewCoins(sdktypes.NewCoin(appconsts.BondDenom, sdkmath.NewInt(fee))))
	}

	accountName, seq, err := signer.SignTransaction(txBuilder)
	if err != nil {
		return nil, "", 0, err
	}

	txBytes, err := signer.EncodeTx(txBuilder.GetTx())
	if err != nil {
		return nil, "", 0, err
	}

	if err := signer.IncrementSequence(accountName); err != nil {
		return nil, "", 0, err
	}

	hash := computeTxHash(txBytes)
	return txBytes, hash, seq, nil
}

// --- Submission (Submitting mode only) ---

// submitNext sends unsubmitted entries to the node.
// Processes as many as possible per tick to maximize throughput.
func (w *worker) submitNext(ctx context.Context) {
	for {
		entry := w.buffer.next()
		if entry == nil {
			return
		}

		_, err := w.v1Client.SendTxToConnection(ctx, w.conn, entry.txBytes)
		if err != nil {
			w.handleSubmitError(err, entry)
			return // Stop on any error.
		}

		entry.submitted = true
	}
}

// handleSubmitError handles errors from sending a tx to the node.
func (w *worker) handleSubmitError(err error, entry *txEntry) {
	kind, expectedSeq := ClassifyBroadcastError(err)
	switch kind {
	case ErrSequenceMismatch:
		w.handleSubmitSequenceMismatch(expectedSeq)

	case ErrMempoolFull, ErrNetworkError:
		// Non-fatal: retry on next tick (entry stays unsubmitted).

	case ErrTxInMempoolCache:
		// Already in mempool — treat as success.
		entry.submitted = true

	case ErrTerminal, ErrInsufficientFee:
		// Fatal: enter Stop mode.
		w.enterStop(entry.sequence, fmt.Errorf("fatal broadcast error: %w", err))
	}
}

// handleSubmitSequenceMismatch handles a sequence mismatch during submission.
func (w *worker) handleSubmitSequenceMismatch(expectedSeq uint64) {
	lastSubmitted := w.buffer.lastSubmittedSeq()

	if expectedSeq <= lastSubmitted {
		// Expected < what we've submitted: reset submission counter.
		// The node wants us to resubmit starting from expectedSeq.
		// But if expectedSeq is below our buffer's first signed entry, we
		// can't satisfy it without re-signing — enter Stop instead of looping.
		front := w.buffer.front()
		if front != nil && expectedSeq < front.sequence {
			w.enterStop(expectedSeq, fmt.Errorf("sequence mismatch: node expects %d, before our buffer (first=%d)", expectedSeq, front.sequence))
			return
		}
		w.buffer.reset(expectedSeq)
		return
	}

	// Expected > last submitted: the node has advanced past us.
	if w.buffer.getBySequence(expectedSeq) != nil {
		// We have a signed tx at the expected seq → Recovery mode.
		w.enterRecovery(expectedSeq)
	} else {
		// We don't have a tx at the expected seq → Stop.
		w.enterStop(expectedSeq, fmt.Errorf("sequence mismatch: node expects %d, beyond our signed range", expectedSeq))
	}
}

// --- Confirmation (all modes) ---

// confirmNext confirms entries by querying TxStatus one-by-one in order.
// Every hash is explicitly queried — no synthesized responses.
//
// Mode semantics:
//   - Submitting: only query submitted entries (unsubmitted will be sent next tick).
//   - Recovering: query Front regardless — needed to detect "hash doesn't exist"
//     contradictions for entries we never submitted.
//   - Stopped: query Front regardless, but only up to stopSeq.
func (w *worker) confirmNext(ctx context.Context) {
	txClient := tx.NewTxClient(w.conn)

	for {
		front := w.buffer.front()
		if front == nil {
			return
		}

		// In Stop mode, don't query past stopSeq — drainRemaining handles those.
		if w.mode == modeStopped && front.sequence >= w.stopSeq {
			return
		}

		// In Submitting mode, only query submitted entries.
		if w.mode == modeSubmitting && !front.submitted {
			return
		}

		resp, err := txClient.TxStatus(ctx, &tx.TxStatusRequest{TxId: front.txHash})
		if err != nil {
			return // Retry on next tick.
		}

		switch resp.Status {
		case core.TxStatusPending:
			// Still in mempool — wait. Stop processing further entries.
			return

		case core.TxStatusCommitted:
			w.handleCommitted(front, resp)

		case core.TxStatusEvicted:
			w.handleEvicted(front)
			// In Submitting mode, the entry was marked unsubmitted; the next
			// loop iteration would hit the "!front.submitted" guard anyway.
			if w.mode == modeSubmitting {
				return
			}
			// In Stop mode, handleEvicted confirmed-front; continue draining.

		case core.TxStatusRejected:
			// Rejected = fatal (requires re-signing).
			w.handleFatalConfirmError(front, fmt.Errorf("tx rejected: %s", resp.Error))
			return

		default:
			// UNKNOWN or any unrecognized status.
			if w.mode == modeRecovering {
				// Canonical contradiction: hash doesn't exist on the node →
				// someone else submitted at this sequence. Exit.
				w.drainAll(fmt.Errorf("recovery contradiction: unknown status for %s at seq %d", front.txHash, front.sequence))
			} else {
				w.handleFatalConfirmError(front, fmt.Errorf("unknown tx status for %s: %s", front.txHash, resp.Status))
			}
			return
		}
	}
}

// handleCommitted processes a committed tx status.
func (w *worker) handleCommitted(entry *txEntry, resp *tx.TxStatusResponse) {
	txResp := &sdktypes.TxResponse{
		TxHash:    entry.txHash,
		Height:    resp.Height,
		Code:      resp.ExecutionCode,
		GasWanted: resp.GasWanted,
		GasUsed:   resp.GasUsed,
		Codespace: resp.Codespace,
	}

	var confirmErr error
	if resp.ExecutionCode != 0 {
		confirmErr = fmt.Errorf("tx execution failed with code %d: %s", resp.ExecutionCode, resp.Error)
	}

	confirmed := w.buffer.confirmFront()
	if confirmed != nil {
		w.sendConfirmed(confirmed.request, txResp, confirmErr)
	}

	// In Recovery mode: check if we've reached the target.
	if w.mode == modeRecovering && w.buffer.lastConfirmed() >= w.recoverTarget-1 {
		w.mode = modeSubmitting
	}
}

// handleEvicted processes an evicted tx status.
func (w *worker) handleEvicted(entry *txEntry) {
	switch w.mode {
	case modeRecovering:
		// During recovery, eviction is a contradiction — our tx was dropped
		// while the node expects a higher sequence. Exit.
		w.drainAll(fmt.Errorf("recovery contradiction: tx evicted at seq %d", entry.sequence))

	case modeStopped:
		// Stop mode doesn't resubmit, so an evicted tx can never be confirmed.
		// Treat as fatal: lower stopSeq if earlier, then confirm-front with error.
		err := fmt.Errorf("tx evicted at seq %d during stop drain", entry.sequence)
		if entry.sequence < w.stopSeq {
			w.stopSeq = entry.sequence
			w.stopErr = err
		}
		confirmed := w.buffer.confirmFront()
		if confirmed != nil {
			w.sendConfirmed(confirmed.request, nil, err)
		}

	default: // modeSubmitting
		// Mark as not-submitted so it gets resubmitted on the next submit tick.
		entry.submitted = false
	}
}

// handleFatalConfirmError handles a fatal error discovered during confirmation.
func (w *worker) handleFatalConfirmError(entry *txEntry, err error) {
	switch w.mode {
	case modeRecovering:
		// Contradiction during recovery → exit.
		w.drainAll(fmt.Errorf("recovery error at seq %d: %w", entry.sequence, err))

	case modeStopped:
		// Another error before stopSeq → lower the stop counter.
		if entry.sequence < w.stopSeq {
			w.stopSeq = entry.sequence
			w.stopErr = err
		}
		// Confirm this entry as failed and remove it.
		confirmed := w.buffer.confirmFront()
		if confirmed != nil {
			w.sendConfirmed(confirmed.request, nil, err)
		}

	default: // modeSubmitting
		w.enterStop(entry.sequence, err)
	}
}

// --- Mode transitions ---

// enterRecovery transitions to Recovery mode.
// Submissions are paused; confirmation continues one-by-one up to target.
func (w *worker) enterRecovery(targetSeq uint64) {
	w.mode = modeRecovering
	w.recoverTarget = targetSeq
}

// enterStop transitions to Stop mode.
// Submissions are paused; confirmation continues up to stopSeq, then drain.
func (w *worker) enterStop(seq uint64, err error) {
	if w.mode == modeStopped {
		// Already stopped — lower the stop counter if this is earlier.
		if seq < w.stopSeq {
			w.stopSeq = seq
			w.stopErr = err
		}
		return
	}
	w.mode = modeStopped
	w.stopSeq = seq
	w.stopErr = err
}

// allConfirmedUpToStop returns true when all entries before stopSeq have been confirmed.
func (w *worker) allConfirmedUpToStop() bool {
	front := w.buffer.front()
	if front == nil {
		return true
	}
	return front.sequence >= w.stopSeq
}

// drainRemaining errors all remaining entries (from stopSeq onwards) and pending requests.
// Called when Stop mode has confirmed everything up to stopSeq.
func (w *worker) drainRemaining() {
	// Error all remaining signed entries.
	for w.buffer.signedLen() > 0 {
		entry := w.buffer.confirmFront()
		if entry != nil {
			w.sendConfirmed(entry.request, nil, w.stopErr)
		}
	}
	// Error all pending requests.
	for w.buffer.pendingLen() > 0 {
		req := w.buffer.popPending()
		w.sendConfirmed(req, nil, w.stopErr)
	}
}

// drainAll sends errors to all remaining handles (pending + signed).
func (w *worker) drainAll(err error) {
	for w.buffer.pendingLen() > 0 {
		req := w.buffer.popPending()
		w.sendConfirmed(req, nil, err)
	}
	for w.buffer.signedLen() > 0 {
		entry := w.buffer.confirmFront()
		if entry != nil {
			w.sendConfirmed(entry.request, nil, err)
		}
	}
}

// --- Helpers ---

// sendConfirmed delivers the terminal result to the caller via the handle.
func (w *worker) sendConfirmed(req *TxRequest, resp *sdktypes.TxResponse, err error) {
	req.resolve(resp, err)
}

// computeTxHash computes the hex-encoded SHA256 hash of tx bytes.
func computeTxHash(txBytes []byte) string {
	// Check if this is a blob tx and extract the inner tx for hashing.
	blobTx, isBlobTx, err := blobtx.UnmarshalBlobTx(txBytes)
	if isBlobTx && err == nil {
		txBytes = blobTx.Tx
	}

	sum := sha256.Sum256(txBytes)
	return hex.EncodeToString(sum[:])
}
