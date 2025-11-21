package v2

import (
	"context"
	"fmt"
	"strings"
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

// sequentialQueue manages single-threaded transaction submission with a unified queue
type sequentialQueue struct {
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	client      *TxClient
	accountName string
	pollTime    time.Duration

	// Single unified queue - transactions stay here until confirmed
	mu           sync.RWMutex
	queue        []*queuedTx    // All transactions from submission to confirmation
	ResignChan   chan *queuedTx // Channel for all rejected transactions that need to be resigned
	ResubmitChan chan *queuedTx // Channel for all evicted transactions that need to be resubmitted

	// Track last confirmed sequence for rollback logic
	lastConfirmedSeq uint64
}

// queuedTx represents a transaction in the queue (from submission to confirmation)
type queuedTx struct {
	// Original submission data
	blobs    []*share.Blob
	options  []user.TxOption
	resultsC chan SequentialSubmissionResult

	// Set after broadcast
	txHash       string    // Empty until broadcast
	txBytes      []byte    // Set after broadcast, used for eviction resubmission
	sequence     uint64    // Set after broadcast
	submittedAt  time.Time // Set after broadcast
	shouldResign bool      // Set after broadcast
}

const (
	defaultSequentialQueueSize = 100
)

func newSequentialQueue(client *TxClient, accountName string, pollTime time.Duration) *sequentialQueue {
	if pollTime == 0 {
		pollTime = user.DefaultPollTime
	}

	ctx, cancel := context.WithCancel(context.Background())
	q := &sequentialQueue{
		client:       client,
		accountName:  accountName,
		pollTime:     pollTime,
		ctx:          ctx,
		cancel:       cancel,
		queue:        make([]*queuedTx, 0, defaultSequentialQueueSize),
		ResignChan:   make(chan *queuedTx, 10), // Buffered channel for resign requests
		ResubmitChan: make(chan *queuedTx, 10), // Buffered channel for resubmit requests
	}
	return q
}

// start begins the sequential queue processing
func (q *sequentialQueue) start() {
	q.wg.Add(2)
	go func() {
		defer q.wg.Done()
		q.coordinate()
	}()
	go func() {
		defer q.wg.Done()
		q.monitorLoop()
	}()
}

// submitJob adds a new transaction to the queue
func (q *sequentialQueue) submitJob(job *SequentialSubmissionJob) {
	q.mu.Lock()
	defer q.mu.Unlock()

	qTx := &queuedTx{
		blobs:    job.Blobs,
		options:  job.Options,
		resultsC: job.ResultsC,
	}

	q.queue = append(q.queue, qTx)
}

// GetQueueSize returns the number of transactions in the queue
func (q *sequentialQueue) GetQueueSize() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.queue)
}

// processNextTx signs and broadcasts the next unbroadcast transaction in queue
func (q *sequentialQueue) processNextTx() {
	// Find first unbroadcast transaction (txHash is empty)
	fmt.Println("Processing next tx")
	var qTx *queuedTx
	q.mu.RLock()
	for _, tx := range q.queue {
		if tx.txHash == "" {
			qTx = tx
			break
		}
	}
	q.mu.RUnlock()

	if qTx == nil {
		return
	}

	resp, err := q.client.BroadcastPayForBlobWithoutRetry(
		q.ctx,
		q.accountName,
		qTx.blobs,
		qTx.options...,
	)

	if err != nil || resp.Code != 0 {
		// Check if this is a sequence mismatch AND we're blocked
		// This means the sequence was rolled back while we were broadcasting
		// TODO: ma\ybe we can check if q is blocked and if so, return
		// otherwise it could mean client is stalled
		if IsSequenceMismatchError(err) {
			fmt.Println("Sequence mismatch error")
			// return we probably need to resign earlier transactions
			// come back to this later
			return
		}

		// Other broadcast errors - send error and remove from queue
		select {
		case qTx.resultsC <- SequentialSubmissionResult{
			Error: fmt.Errorf("broadcast failed: %w", err),
		}:
		case <-q.ctx.Done():
		}
		q.removeFromQueue(qTx)
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	// Broadcast successful - mark as broadcast in queue
	sequence := q.client.Signer().Account(q.accountName).Sequence()
	txBytes := q.client.TxClient.TxTracker.GetTxBytes(q.accountName, sequence)

	qTx.txHash = resp.TxHash
	qTx.txBytes = txBytes
	qTx.sequence = sequence
	qTx.submittedAt = time.Now()
	fmt.Println("Broadcast successful - marking as broadcast in queue")
}

// monitorLoop periodically checks the status of broadcast transactions
func (q *sequentialQueue) monitorLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			return
		case <-ticker.C:
			q.checkBroadcastTransactions()
		}
	}
}

// coordinate coordinates transaction submission with confirmation
func (q *sequentialQueue) coordinate() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			return
		case <-q.ResignChan:
			fmt.Println("Resigning rejected tx")
			q.ResignRejected()
		case qTx := <-q.ResubmitChan:
			fmt.Println("Resubmitting evicted tx")
			q.ResubmitEvicted(qTx)
		case <-ticker.C:
			q.processNextTx()
		}
	}
}

// ResignRejected resigns a rejected transaction
func (q *sequentialQueue) ResignRejected() {
	fmt.Println("Resigning rejected tx")
	q.mu.RLock()
	var txsToResign []*queuedTx
	for _, qTx := range q.queue {
		if qTx.shouldResign {
			txsToResign = append(txsToResign, qTx)
		}
	}
	q.mu.RUnlock()

	for _, qTx := range txsToResign {
		if qTx.shouldResign {
			// resign the tx
			resp, err := q.client.BroadcastPayForBlobWithoutRetry(
				q.ctx,
				q.accountName,
				qTx.blobs,
				qTx.options...,
			)
			if err != nil {
				// send error and remove from queue
				select {
				case qTx.resultsC <- SequentialSubmissionResult{
					Error: fmt.Errorf("rejected and failed to resign: %w", err),
				}:
				case <-q.ctx.Done():
				}
				q.removeFromQueue(qTx)
				return
			}
			sequence := q.client.Signer().Account(q.accountName).Sequence()
			txBytes := q.client.TxClient.TxTracker.GetTxBytes(q.accountName, sequence)

			qTx.txHash = resp.TxHash
			qTx.txBytes = txBytes
			qTx.sequence = sequence
			qTx.shouldResign = false
			fmt.Printf("Resigned and submitted tx successfully: %s\n", resp.TxHash)
		}
	}
}

// TODO: come back to this and see if it makes sense
// func (q *sequentialQueue) setTxInfo(qTx *queuedTx, resp *sdktypes.TxResponse, txBytes []byte, sequence uint64) {
// 	q.mu.Lock()
// 	defer q.mu.Unlock()

// 	qTx.txHash = resp.TxHash
// 	qTx.txBytes = txBytes
// 	qTx.sequence = sequence
// 	qTx.shouldResign = false
// }

func (q *sequentialQueue) ResubmitEvicted(qTx *queuedTx) {
	fmt.Println("Resubmitting evicted tx")
	q.mu.RLock()
	txBytes := qTx.txBytes
	q.mu.RUnlock()

	// check if the tx needs to be resubmitted
	resubmitResp, err := q.client.ResubmitTxBytes(q.ctx, txBytes)
	if err != nil || resubmitResp.Code != 0 {
		// send error and remove from queue
		select {
		case qTx.resultsC <- SequentialSubmissionResult{
			Error: fmt.Errorf("evicted and failed to resubmit: %w", err),
		}:
		case <-q.ctx.Done():
		}
		q.removeFromQueue(qTx)
		return
	}
}

// checkBroadcastTransactions checks status of all broadcast transactions
func (q *sequentialQueue) checkBroadcastTransactions() {
	fmt.Println("Checking broadcast transactions")
	q.mu.RLock()
	// Collect all broadcast transactions (those with non-empty txHash)
	var broadcastTxs []*queuedTx // TODO: cap the size
	for _, tx := range q.queue {
		if tx.txHash != "" {
			broadcastTxs = append(broadcastTxs, tx)
		}
	}
	q.mu.RUnlock()

	if len(broadcastTxs) == 0 {
		return
	}

	// Create tx client for status queries
	txClient := tx.NewTxClient(q.client.GetGRPCConnection())

	for _, qTx := range broadcastTxs {
		statusResp, err := txClient.TxStatus(q.ctx, &tx.TxStatusRequest{TxId: qTx.txHash})
		if err != nil {
			continue // Network error, try again later
		}

		switch statusResp.Status {
		case core.TxStatusCommitted:
			q.handleCommitted(qTx, statusResp)
		case core.TxStatusEvicted:
			fmt.Println("Handling evicted tx")
			q.ResubmitChan <- qTx
		case core.TxStatusRejected:
			q.handleRejected(qTx, statusResp, txClient)
		}
	}
}

// handleCommitted processes a confirmed transaction
func (q *sequentialQueue) handleCommitted(qTx *queuedTx, statusResp *tx.TxStatusResponse) {
	fmt.Println("Handling confirmed tx")
	// Check execution code
	if statusResp.ExecutionCode != abci.CodeTypeOK {
		// Execution failed
		select {
		case <-q.ctx.Done():
		case qTx.resultsC <- SequentialSubmissionResult{
			Error: fmt.Errorf("tx execution failed with code %d: %s", statusResp.ExecutionCode, statusResp.Error),
		}:
		}
		q.removeFromQueue(qTx)
		return
	}

	// Success - send result
	select {
	case <-q.ctx.Done():
		return
	case qTx.resultsC <- SequentialSubmissionResult{
		TxResponse: &sdktypes.TxResponse{
			Height:    statusResp.Height,
			TxHash:    qTx.txHash,
			Code:      statusResp.ExecutionCode,
			Codespace: statusResp.Codespace,
			GasWanted: statusResp.GasWanted,
			GasUsed:   statusResp.GasUsed,
			Signers:   statusResp.Signers,
		},
		Error: nil,
	}:
	}

	// Update last confirmed sequence
	q.setLastConfirmedSeq(qTx.sequence)
	q.removeFromQueue(qTx)
}

func (q *sequentialQueue) setLastConfirmedSeq(seq uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.lastConfirmedSeq = seq
}

// handleRejected processes a rejected transaction
func (q *sequentialQueue) handleRejected(qTx *queuedTx, statusResp *tx.TxStatusResponse, txClient tx.TxClient) {
	fmt.Println("Handling rejected tx")
	// Step 1: Roll back sequence if previous tx was confirmed
	if q.isPreviousTxConfirmed(qTx.sequence) {
		q.mu.Lock()
		q.client.Signer().SetSequence(q.accountName, qTx.sequence)
		q.mu.Unlock()
	}

	isNonceMismatch := isSequenceMismatchRejection(statusResp.Error)
	if isNonceMismatch {
		q.mu.Lock()
		qTx.shouldResign = true
		q.mu.Unlock()
	}

	// Step 2: Collect subsequent transactions to check
	q.mu.RLock()
	var subsequentTxs []*queuedTx
	for _, subTx := range q.queue {
		if subTx.sequence > qTx.sequence && subTx.txHash != "" {
			subsequentTxs = append(subsequentTxs, subTx)
		}
	}
	q.mu.RUnlock()

	// Step 3: Batch query subsequent transactions to see if they were also rejected // TODO: in future this should be handled by batch txstatus request
	for _, subTx := range subsequentTxs {
		if subTx.sequence > qTx.sequence && subTx.txHash != "" {
			// TODO: this should also be rejected for sequence mismatch
			resp, err := txClient.TxStatus(q.ctx, &tx.TxStatusRequest{TxId: subTx.txHash})
			if err == nil && resp.Status == core.TxStatusRejected && resp.ExecutionCode == 32 {
				fmt.Println("Sequence mismatch error: ReCheck()")
				q.mu.Lock()
				subTx.shouldResign = true
				q.mu.Unlock()
			}
		}
	}
	// Q: should we wait till all txs are marked for resign before sending to resign channel?
	q.ResignChan <- qTx

	if !isNonceMismatch {
		// Non-nonce error remove from queue and return error back to user
		select {
		case <-q.ctx.Done():
		case qTx.resultsC <- SequentialSubmissionResult{
			Error: fmt.Errorf("tx rejected: %s", statusResp.Error),
		}:
		}
		q.removeFromQueue(qTx)
		return
	}

}

// removeFromQueue removes a transaction from the queue
func (q *sequentialQueue) removeFromQueue(qTx *queuedTx) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, tx := range q.queue {
		if tx == qTx {
			q.queue = append(q.queue[:i], q.queue[i+1:]...)
			return
		}
	}
}

// isPreviousTxConfirmed checks if the previous transaction was confirmed
func (q *sequentialQueue) isPreviousTxConfirmed(seq uint64) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if seq == 0 {
		return true
	}
	return q.lastConfirmedSeq >= seq-1
}

// isSequenceMismatchRejection checks if an error message indicates sequence mismatch
func isSequenceMismatchRejection(errMsg string) bool {
	return strings.Contains(errMsg, "account sequence mismatch") ||
		strings.Contains(errMsg, "incorrect account sequence")
}
