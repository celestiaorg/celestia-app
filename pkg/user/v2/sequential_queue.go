package v2

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/go-square/v3/share"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/rpc/core"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
)

// SequentialSubmissionJob represents a transaction submission task
type SequentialSubmissionJob struct {
	Blobs    []*share.Blob
	Options  []user.TxOption
	Ctx      context.Context
	ResultsC chan SequentialSubmissionResult
}

// SequentialSubmissionResult contains the result of a transaction submission
type SequentialSubmissionResult struct {
	TxResponse *sdktypes.TxResponse
	Error      error
}

// sequentialQueue manages single-threaded transaction submission
type sequentialQueue struct {
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	client      *TxClient
	accountName string
	pollTime    time.Duration

	// Transaction processing
	mu         sync.RWMutex
	queuedTxs  map[uint64]*queuedTx  // Transactions waiting to be broadcast
	pendingTxs map[uint64]*pendingTx // Transactions broadcast, waiting confirmation
}

// queuedTx represents a transaction waiting to be broadcast
type queuedTx struct {
	sequence    uint64
	blobs       []*share.Blob
	options     []user.TxOption
	resultsC    chan SequentialSubmissionResult
	needsResign bool // True if needs resigning (e.g., after prior rejection)
}

// pendingTx tracks a transaction that has been broadcast
type pendingTx struct {
	sequence    uint64
	status      string
	txHash      string
	txBytes     []byte // Stored for resubmission without resigning
	blobs       []*share.Blob
	options     []user.TxOption
	resultsC    chan SequentialSubmissionResult
	submittedAt time.Time
	attempts    int
}

const (
	defaultSequentialQueueSize = 100
	maxResubmitAttempts        = 5
)

func newSequentialQueue(client *TxClient, accountName string, pollTime time.Duration) *sequentialQueue {
	if pollTime == 0 {
		pollTime = user.DefaultPollTime
	}

	return &sequentialQueue{
		client:      client,
		accountName: accountName,
		pollTime:    pollTime,
		queuedTxs:   make(map[uint64]*queuedTx),
		pendingTxs:  make(map[uint64]*pendingTx),
	}
}

// start initiates the sequential queue processor
func (q *sequentialQueue) start(ctx context.Context) error {
	q.ctx, q.cancel = context.WithCancel(ctx)

	// Start the processing loop (broadcasts queued txs in order)
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		q.processLoop()
	}()

	// Start the monitoring loop (confirms pending txs)
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		q.monitorLoop()
	}()

	return nil
}

// stop shuts down the sequential queue
func (q *sequentialQueue) stop() {
	if q.ctx == nil {
		return
	}

	q.cancel()
	q.wg.Wait()
	q.ctx, q.cancel = nil, nil
}

// isStarted returns whether the queue is started
func (q *sequentialQueue) isStarted() bool {
	return q.ctx != nil && q.cancel != nil
}

// submitJob submits a job to the sequential queue (directly, no channel)
func (q *sequentialQueue) submitJob(job *SequentialSubmissionJob) {
	if !q.isStarted() {
		job.ResultsC <- SequentialSubmissionResult{Error: fmt.Errorf("sequential queue not started")}
		return
	}

	// Check if job context is cancelled
	if job.Ctx.Err() != nil {
		job.ResultsC <- SequentialSubmissionResult{Error: job.Ctx.Err()}
		return
	}

	// Get current sequence
	acc := q.client.Account(q.accountName)
	if acc == nil {
		job.ResultsC <- SequentialSubmissionResult{Error: fmt.Errorf("account %s not found", q.accountName)}
		return
	}
	sequence := acc.Sequence()

	// Add to queued transactions
	q.mu.Lock()
	q.queuedTxs[sequence] = &queuedTx{
		sequence:    sequence,
		blobs:       job.Blobs,
		options:     job.Options,
		resultsC:    job.ResultsC,
		needsResign: false,
	}
	q.mu.Unlock()

	// Increment sequence for next job
	if err := q.client.Signer().IncrementSequence(q.accountName); err != nil {
		job.ResultsC <- SequentialSubmissionResult{Error: fmt.Errorf("error incrementing sequence: %w", err)}
	}
}

// processLoop is a process continuely submitting txs from Q
func (q *sequentialQueue) processLoop() {
	// TODO: Maybe this should be like 1 second? so tx client can actually be the one to control submission cadence rather than user?
	ticker := time.NewTicker(q.pollTime / 2) // ARBITRARY DELAY
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			q.submitTxsInQueue()
		case <-q.ctx.Done():
			return
		}
	}
}

// submitTxsInQueue broadcasts the next transaction in sequence order
// this needs to change to only lock per transaction submission rather than the whole queue
func (q *sequentialQueue) submitTxsInQueue() {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Get the first transaction from the queue
	// keep one second delay between submissions
	for _, queued := range q.queuedTxs {

		// Broadcast the transaction
		var resp *sdktypes.TxResponse
		// TODO: figure out how to set tx bytes
		var txBytes []byte
		var err error

		resp, err = q.client.BroadcastPayForBlobWithoutRetry(q.ctx, q.accountName, queued.blobs, queued.options...)
		if err != nil {
			// check if the error is sequence mismatch and if tx should be resigned. (never resign evicted txs)
			queued.resultsC <- SequentialSubmissionResult{Error: fmt.Errorf("broadcast failed: %w", err)}
			delete(q.queuedTxs, queued.sequence)
			return
		}

		// Move from queued to pending
		q.pendingTxs[queued.sequence] = &pendingTx{
			sequence:    queued.sequence,
			txHash:      resp.TxHash,
			txBytes:     txBytes,
			blobs:       queued.blobs,
			options:     queued.options,
			resultsC:    queued.resultsC,
			submittedAt: time.Now(),
			attempts:    1,
		}
		delete(q.queuedTxs, queued.sequence)

		time.Sleep(time.Second) // this is submission delay but could be done differently
	}
}

// monitorLoop periodically checks pending transactions
func (q *sequentialQueue) monitorLoop() {
	ticker := time.NewTicker(q.pollTime)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			q.checkPendingTransactions()
		case <-q.ctx.Done():
			return
		}
	}
}

// checkPendingTransactions checks status of all pending transactions
func (q *sequentialQueue) checkPendingTransactions() {
	// todo; possibly error prone
	txClient := tx.NewTxClient(q.client.GetConn())

	// Check all pending transactions
	for _, pending := range q.pendingTxs {
		seq := pending.sequence

		// Check transaction status
		statusResp, err := txClient.TxStatus(q.ctx, &tx.TxStatusRequest{TxId: pending.txHash})
		if err != nil {
			// Network error, skip this check
			continue
		}

		switch statusResp.Status {
		case core.TxStatusPending:
			// Still pending, continue monitoring
			continue

		case core.TxStatusCommitted:
			// Check execution code
			if statusResp.ExecutionCode != abci.CodeTypeOK {
				// Execution failed - treat like rejection, roll back signer sequence
				// should prolly use execution error type same as in tx client
				pending.resultsC <- SequentialSubmissionResult{
					Error: fmt.Errorf("tx execution failed with code %d: %s", statusResp.ExecutionCode, statusResp.Error),
				}
				continue
			}
			// use populate tx response from tx client TODO: reuse the function that populates
			pending.resultsC <- SequentialSubmissionResult{
				TxResponse: &sdktypes.TxResponse{
					Height:    statusResp.Height,
					TxHash:    pending.txHash,
					Code:      statusResp.ExecutionCode,
					Codespace: statusResp.Codespace,
					GasWanted: statusResp.GasWanted,
					GasUsed:   statusResp.GasUsed,
					Signers:   statusResp.Signers,
				},
				Error: nil,
			}

		case core.TxStatusEvicted:
			// Transaction evicted - put back in queue for resubmission WITHOUT resigning
			// Add back to queuedTxs with same sequence
			q.mu.Lock()
			q.queuedTxs[seq] = &queuedTx{
				sequence:    seq,
				blobs:       pending.blobs,
				options:     pending.options,
				resultsC:    pending.resultsC,
				needsResign: false, // Keep existing tx bytes, no resigning
			}
			delete(q.pendingTxs, seq)
			q.mu.Unlock()
		case core.TxStatusRejected:
			// tx rejected - roll back the signer sequence
			// check if the tx before was also rejected, if so, no need to roll back the signer sequence
			// because it would have been rolled back already
			prevTx, exists := q.pendingTxs[seq-1] // TODO: we need accessors with mutexes for this
			if !exists || prevTx.status != core.TxStatusRejected {
				// no previous tx, so we can roll back the signer sequence
				q.client.TxClient.Signer().SetSequence(q.accountName, seq)
				continue
			}
			pending.status = core.TxStatusRejected
			// TODO: how do i do this? maybe i should keep track of tx hashes and just tx status them
			pending.resultsC <- SequentialSubmissionResult{
				Error: fmt.Errorf("tx rejected: %s", statusResp.Error),
			}
			// TODO: remove the bytes from the tx tracker and just keep the sequence, hash and status
			// this is to save storage space

			// Mark subsequent transactions for resigning
			q.markForResign(seq)
		}
	}

}

// markForResign marks all subsequent queued transactions for resigning
// idk if this is the best way, we shall see
func (q *sequentialQueue) markForResign(fromSeq uint64) {
	for seq, queued := range q.queuedTxs {
		if seq > fromSeq {
			queued.needsResign = true
		}
	}
}

// purgeOldTxs removes rejected/confirmed transactions that are older than 10 minutes (possible refactor this to height since time is the worst in blockchain env
// but here it feels not too bad since its only for cleanup)
// TODO: find out how much storage is required for this to work.
// we could possibly remove everything but sequence, status and hash for confirmed/rejected txs for lightweight storage.
func (q *sequentialQueue) purgeOldTxs() {
	q.mu.Lock()
	defer q.mu.Unlock()

	// remove all transactions that are older than 10 minutes
	for seq, pending := range q.pendingTxs {
		if time.Since(pending.submittedAt) > 10*time.Minute {
			delete(q.pendingTxs, seq)
			delete(q.queuedTxs, seq)
		}
	}
}

// GetQueueSize returns the number of pending transactions
func (q *sequentialQueue) GetQueueSize() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.queuedTxs) + len(q.pendingTxs)
}
