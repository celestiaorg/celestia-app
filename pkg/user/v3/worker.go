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
)

const (
	maxConfirmBatch = 20
	signTickRate    = 50 * time.Millisecond
	submitTickRate  = 100 * time.Millisecond
)

// worker is the single background goroutine that owns all mutable state
// for the async pipeline: buffer and node connections.
type worker struct {
	v1Client    *user.TxClient
	buffer      *TxBuffer
	nodes       []*NodeConnection
	requestCh   <-chan *TxRequest
	pollTime    time.Duration
	accountName string
}

// run is the main event loop for the worker goroutine.
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
			w.buffer.AddPending(req)

		case <-signTicker.C:
			w.signPending(ctx)

		case <-submitTicker.C:
			w.submitToNodes(ctx)

		case <-confirmTicker.C:
			w.confirmSubmitted(ctx)
		}
	}
}

// signPending signs all pending requests.
func (w *worker) signPending(ctx context.Context) {
	for w.buffer.PendingLen() > 0 {
		req := w.buffer.PopPending()
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
			sequence:    seq,
			txHash:      txHash,
			txBytes:     txBytes,
			request:     req,
			submittedTo: make(map[int]bool),
		}
		if err := w.buffer.AppendSigned(entry); err != nil {
			w.sendConfirmed(req, nil, fmt.Errorf("buffer append: %w", err))
			continue
		}

		// Send signed callback.
		select {
		case req.signedCh <- SignedResult{TxHash: txHash, Sequence: seq}:
		default:
		}
		close(req.signedCh)
	}
}

// signRequest signs a single request, handling both PFB and regular msg paths.
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

	// Increment sequence after signing.
	if err := signer.IncrementSequence(w.accountName); err != nil {
		return nil, "", 0, err
	}

	hash := computeTxHash(txBytes)
	return txBytes, hash, seq, nil
}

// signRegular signs a regular (non-PFB) transaction.
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
			if ok, seqErr := w.v1Client.HandleSequenceMismatch(err, txBuilder); !ok {
				return nil, "", 0, seqErr
			}
			gasLimit, err = w.v1Client.EstimateGasForTx(ctx, txBuilder)
			if err != nil {
				return nil, "", 0, fmt.Errorf("retrying gas estimation: %w", err)
			}
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

// submitToNodes submits signed entries to all active nodes.
func (w *worker) submitToNodes(ctx context.Context) {
	for _, entry := range w.buffer.signed {
		for _, node := range w.nodes {
			if !node.IsAvailable() || !node.NeedsSubmission(entry.sequence) {
				continue
			}
			if entry.submittedTo[node.id] {
				continue
			}

			resp, err := w.v1Client.SendTxToConnection(ctx, node.conn, entry.txBytes)
			if err != nil {
				w.handleSubmitError(ctx, err, &entry, node)
				continue
			}

			_ = resp
			entry.submittedTo[node.id] = true
			if entry.firstSubmitAt.IsZero() {
				entry.firstSubmitAt = time.Now()
			}
			node.RecordSubmission(entry.sequence)
			node.ResetFailures()

			// Update the entry in the buffer.
			if bufEntry := w.buffer.GetByHash(entry.txHash); bufEntry != nil {
				bufEntry.submittedTo[node.id] = true
				if bufEntry.firstSubmitAt.IsZero() {
					bufEntry.firstSubmitAt = entry.firstSubmitAt
				}
			}

			// Send submitted callback (once, on first node).
			if len(entry.submittedTo) == 1 {
				select {
				case entry.request.submittedCh <- SubmittedResult{TxHash: entry.txHash}:
				default:
				}
				close(entry.request.submittedCh)
			}
		}
	}
}

// handleSubmitError handles errors from submitting a tx to a node.
func (w *worker) handleSubmitError(ctx context.Context, err error, entry *txEntry, node *NodeConnection) {
	kind, expectedSeq := ClassifyBroadcastError(err)
	switch kind {
	case ErrSequenceMismatch:
		w.handleSequenceMismatch(ctx, expectedSeq, entry)
	case ErrMempoolFull, ErrNetworkError:
		node.MarkRecovering()
	case ErrTxInMempoolCache:
		// Treat as success — already in mempool.
		entry.submittedTo[node.id] = true
		if bufEntry := w.buffer.GetByHash(entry.txHash); bufEntry != nil {
			bufEntry.submittedTo[node.id] = true
		}
	case ErrTerminal, ErrInsufficientFee:
		w.sendConfirmed(entry.request, nil, fmt.Errorf("terminal broadcast error: %w", err))
		w.buffer.ConfirmFront() // remove from buffer
	}
}

// handleSequenceMismatch handles a sequence mismatch during submission.
func (w *worker) handleSequenceMismatch(ctx context.Context, expectedSeq uint64, entry *txEntry) {
	signer := w.v1Client.Signer()
	acc, exists := signer.GetAccount(w.accountName)
	if !exists {
		return
	}
	currentSeq := acc.Sequence()

	if expectedSeq < currentSeq {
		// Case 1: expected < client — prior tx evicted/rejected
		w.recoverLowerSequence(ctx, expectedSeq)
	} else if expectedSeq > currentSeq {
		// Case 2: expected > client — node advanced beyond us
		w.recoverHigherSequence(ctx, expectedSeq, entry)
	}
}

// recoverLowerSequence handles Case 1: expected sequence < client sequence.
// Checks status of txs in [expected, last_submitted] and handles evicted/rejected.
func (w *worker) recoverLowerSequence(ctx context.Context, expectedSeq uint64) {
	signer := w.v1Client.Signer()
	acc, exists := signer.GetAccount(w.accountName)
	if !exists {
		return
	}
	lastSubmitted := acc.Sequence() - 1 // sequence was already incremented after signing

	entries := w.buffer.EntriesInRange(expectedSeq, lastSubmitted)
	if len(entries) == 0 {
		return
	}

	txClient := tx.NewTxClient(w.v1Client.Conns()[0])
	for _, entry := range entries {
		resp, err := txClient.TxStatus(ctx, &tx.TxStatusRequest{TxId: entry.txHash})
		if err != nil {
			continue
		}
		switch resp.Status {
		case core.TxStatusEvicted:
			// Resubmit original bytes on next submit tick (reset submittedTo).
			if bufEntry := w.buffer.GetByHash(entry.txHash); bufEntry != nil {
				bufEntry.submittedTo = make(map[int]bool)
			}
		case core.TxStatusRejected:
			// Rollback sequence and notify caller.
			w.v1Client.Lock()
			_ = signer.SetSequence(w.accountName, entry.sequence)
			w.v1Client.Unlock()

			removed := w.buffer.RollbackTo(entry.sequence)
			for _, r := range removed {
				w.sendConfirmed(r.request, nil, fmt.Errorf("tx rejected during sequence recovery: %s", resp.Error))
			}
			return
		}
	}
}

// recoverHigherSequence handles Case 2: expected sequence > client sequence.
func (w *worker) recoverHigherSequence(ctx context.Context, expectedSeq uint64, entry *txEntry) {
	txClient := tx.NewTxClient(w.v1Client.Conns()[0])
	resp, err := txClient.TxStatus(ctx, &tx.TxStatusRequest{TxId: entry.txHash})
	if err != nil {
		return
	}

	if resp.Status == core.TxStatusCommitted {
		// Advance state — the tx was committed by another path.
		w.v1Client.Lock()
		signer := w.v1Client.Signer()
		_ = signer.SetSequence(w.accountName, expectedSeq)
		w.v1Client.Unlock()

		// Remove confirmed entries from buffer.
		for {
			front := w.buffer.Front()
			if front == nil || front.sequence >= expectedSeq {
				break
			}
			confirmed := w.buffer.ConfirmFront()
			if confirmed != nil {
				w.sendConfirmed(confirmed.request, &sdktypes.TxResponse{
					TxHash: confirmed.txHash,
					Height: resp.Height,
					Code:   resp.ExecutionCode,
				}, nil)
			}
		}
	} else {
		// Terminal error: sequence is ahead but tx not committed.
		w.sendConfirmed(entry.request, nil, fmt.Errorf("sequence mismatch: node expects %d but tx not committed", expectedSeq))
	}
}

// confirmSubmitted polls TxStatus for submitted entries.
func (w *worker) confirmSubmitted(ctx context.Context) {
	hashes := w.buffer.SubmittedHashes(maxConfirmBatch)
	if len(hashes) == 0 {
		return
	}

	txClient := tx.NewTxClient(w.v1Client.Conns()[0])

	if len(hashes) == 1 {
		w.confirmSingle(ctx, txClient, hashes[0])
		return
	}

	resp, err := txClient.TxStatusBatch(ctx, &tx.TxStatusBatchRequest{TxIds: hashes})
	if err != nil {
		return // Will retry on next tick.
	}

	for _, result := range resp.Statuses {
		if result.Status == nil {
			continue
		}
		w.processConfirmResult(result.TxHash, result.Status)
	}
}

// confirmSingle handles confirmation for a single tx hash.
func (w *worker) confirmSingle(ctx context.Context, txClient tx.TxClient, hash string) {
	resp, err := txClient.TxStatus(ctx, &tx.TxStatusRequest{TxId: hash})
	if err != nil {
		return
	}
	w.processConfirmResult(hash, resp)
}

// processConfirmResult processes a single TxStatusResponse for confirmation.
func (w *worker) processConfirmResult(txHash string, resp *tx.TxStatusResponse) {
	entry := w.buffer.GetByHash(txHash)
	if entry == nil {
		return
	}

	switch resp.Status {
	case core.TxStatusPending:
		// No action — keep polling.

	case core.TxStatusCommitted:
		txResp := &sdktypes.TxResponse{
			TxHash:    txHash,
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
		// Remove from buffer up to this entry.
		w.confirmUpTo(entry.sequence, txResp, confirmErr)

	case core.TxStatusEvicted:
		// Reset submittedTo so it gets resubmitted on the next submit tick.
		entry.submittedTo = make(map[int]bool)

	case core.TxStatusRejected:
		// Rollback sequence and error all affected entries.
		w.v1Client.Lock()
		_ = w.v1Client.Signer().SetSequence(w.accountName, entry.sequence)
		w.v1Client.Unlock()

		removed := w.buffer.RollbackTo(entry.sequence)
		for _, r := range removed {
			w.sendConfirmed(r.request, nil, fmt.Errorf("tx rejected: %s", resp.Error))
		}

	default:
		// Unknown status — treat as error.
		w.sendConfirmed(entry.request, nil, fmt.Errorf("unknown tx status for %s: %s", txHash, resp.Status))
		// Remove from buffer.
		if front := w.buffer.Front(); front != nil && front.txHash == txHash {
			w.buffer.ConfirmFront()
		}
	}
}

// confirmUpTo confirms all entries up to and including the given sequence.
func (w *worker) confirmUpTo(seq uint64, txResp *sdktypes.TxResponse, err error) {
	for {
		front := w.buffer.Front()
		if front == nil {
			break
		}
		if front.sequence > seq {
			break
		}
		confirmed := w.buffer.ConfirmFront()
		if confirmed == nil {
			break
		}
		if confirmed.sequence == seq {
			w.sendConfirmed(confirmed.request, txResp, err)
		} else {
			// Entries before the target are implicitly confirmed (committed in order).
			w.sendConfirmed(confirmed.request, &sdktypes.TxResponse{
				TxHash: confirmed.txHash,
			}, nil)
		}
	}
}

// drainAll sends errors to all remaining handles.
func (w *worker) drainAll(err error) {
	// Drain pending.
	for w.buffer.PendingLen() > 0 {
		req := w.buffer.PopPending()
		w.sendConfirmed(req, nil, err)
	}
	// Drain signed.
	for w.buffer.SignedLen() > 0 {
		entry := w.buffer.ConfirmFront()
		if entry != nil {
			w.sendConfirmed(entry.request, nil, err)
		}
	}
}

// sendConfirmed sends a ConfirmedResult to the request's callback channel.
func (w *worker) sendConfirmed(req *TxRequest, resp *sdktypes.TxResponse, err error) {
	select {
	case req.confirmedCh <- ConfirmedResult{Response: resp, Err: err}:
	default:
	}
	close(req.confirmedCh)
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
