package v2

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
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
	mu               sync.RWMutex
	queue            []*queuedTx    // All transactions from submission to confirmation
	queueMemoryBytes uint64         // Total memory used by blobs in queue (in bytes)
	maxMemoryBytes   uint64         // Maximum memory allowed for queue (in bytes)
	ResignChan       chan *queuedTx // Channel for all rejected transactions that need to be resigned
	ResubmitChan     chan *queuedTx // Channel for all evicted transactions that need to be resubmitted

	// Track last confirmed sequence for rollback logic
	lastConfirmedSeq uint64

	// Track last rejected sequence for rollback logic
	lastRejectedSeq uint64

	isRecovering atomic.Bool

	// Submission tracking metrics
	newBroadcastCount uint64    // Count of new transaction broadcasts
	resubmitCount     uint64    // Count of resubmissions (evicted txs)
	resignCount       uint64    // Count of resignations (rejected txs)
	lastMetricsLog    time.Time // Last time we logged metrics
	metricsStartTime  time.Time // Start time for rate calculation
}

// queuedTx represents a transaction in the queue (from submission to confirmation)
type queuedTx struct {
	// Original submission data
	blobs    []*share.Blob
	options  []user.TxOption
	resultsC chan SequentialSubmissionResult

	// Set after broadcast
	txHash         string    // Empty until broadcast
	txBytes        []byte    // Set after broadcast, used for eviction resubmission
	sequence       uint64    // Set after broadcast
	submittedAt    time.Time // Set after broadcast
	isResubmitting bool      // True if transaction is currently being resubmitted (prevents duplicates)
}

const (
	// defaultMaxQueueMemoryMB is the maximum memory (in MB) allowed for the queue to prevent OOM
	// This limits the total size of blob data held in memory at once
	defaultMaxQueueMemoryMB = 200 // 100MB default
)

func newSequentialQueue(client *TxClient, accountName string, pollTime time.Duration) *sequentialQueue {
	if pollTime == 0 {
		pollTime = user.DefaultPollTime
	}

	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	q := &sequentialQueue{
		client:           client,
		accountName:      accountName,
		pollTime:         pollTime,
		ctx:              ctx,
		cancel:           cancel,
		queue:            make([]*queuedTx, 0),
		queueMemoryBytes: 0,
		maxMemoryBytes:   defaultMaxQueueMemoryMB * 1024 * 1024, // Convert MB to bytes
		ResubmitChan:     make(chan *queuedTx, 10),
		lastMetricsLog:   now,
		metricsStartTime: now,
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

func (q *sequentialQueue) setRecovering(v bool) {
	// q.mu.Lock()
	q.isRecovering.Store(v)
	// q.mu.Unlock()
}

func (q *sequentialQueue) getRecovering() bool {
	// q.mu.RLock()
	return q.isRecovering.Load()
	// q.mu.RUnlock()
}

// submitJob adds a new transaction to the queue
// It enforces memory limits to prevent OOM by blocking until sufficient memory is available
func (q *sequentialQueue) submitJob(job *SequentialSubmissionJob) {
	// Calculate memory size of this transaction's blobs
	blobsMemory := calculateBlobsMemory(job.Blobs)

	// Wait for memory space in queue (backpressure) - prevents memory exhaustion
	for {
		q.mu.Lock()
		if q.queueMemoryBytes+blobsMemory <= q.maxMemoryBytes {
			// Sufficient memory available - add transaction
			qTx := &queuedTx{
				blobs:    job.Blobs,
				options:  job.Options,
				resultsC: job.ResultsC,
			}
			q.queue = append(q.queue, qTx)
			q.queueMemoryBytes += blobsMemory

			currentMemMB := float64(q.queueMemoryBytes) / (1024 * 1024)
			maxMemMB := float64(q.maxMemoryBytes) / (1024 * 1024)
			q.mu.Unlock()

			// Log when approaching capacity (>80%)
			if q.queueMemoryBytes > (q.maxMemoryBytes * 80 / 100) {
				fmt.Printf("[MEMORY] Queue approaching capacity: %.2f/%.2f MB (%.1f%%)\n",
					currentMemMB, maxMemMB, (currentMemMB/maxMemMB)*100)
			}
			return
		}

		// Queue memory full - unlock and wait for space
		// currentMemMB := float64(q.queueMemoryBytes) / (1024 * 1024)
		// maxMemMB := float64(q.maxMemoryBytes) / (1024 * 1024)
		q.mu.Unlock()

		// fmt.Printf("[BACKPRESSURE] Queue memory full (%.2f/%.2f MB), waiting to prevent OOM\n",
		// currentMemMB, maxMemMB)

		select {
		case <-time.After(100 * time.Millisecond):
			// Wait a bit then retry
		case <-q.ctx.Done():
			// Context cancelled, exit
			return
		}
	}
}

// calculateBlobsMemory returns the total memory size of blobs in bytes
func calculateBlobsMemory(blobs []*share.Blob) uint64 {
	var total uint64
	for _, blob := range blobs {
		if blob != nil {
			total += uint64(len(blob.Data()))
		}
	}
	return total
}

// GetQueueSize returns the number of transactions in the queue
func (q *sequentialQueue) GetQueueSize() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.queue)
}

// processNextTx signs and broadcasts the next unbroadcast transaction in queue
func (q *sequentialQueue) processNextTx() {
	startTime := time.Now()

	fmt.Println("Recovering?", q.getRecovering())
	if q.getRecovering() {
		fmt.Println("Recovering from previous rejection/eviction - skipping current tx")
		return
	}

	scanStart := time.Now()
	var qTx *queuedTx
	q.mu.RLock()
	for _, tx := range q.queue {
		if tx.txHash == "" {
			qTx = tx
			break
		}
	}
	queueSize := len(q.queue)
	q.mu.RUnlock()
	scanDuration := time.Since(scanStart)

	if qTx == nil {
		return
	}

	fmt.Printf("[TIMING] Queue scan took %v (queue size: %d)\n", scanDuration, queueSize)

	// Log current signer sequence before broadcast
	currentSeq := q.client.Signer().Account(q.accountName).Sequence()
	fmt.Printf("[DEBUG] Attempting broadcast with signer sequence: %d\n", currentSeq)

	broadcastStart := time.Now()
	resp, txBytes, err := q.client.BroadcastPayForBlobWithoutRetry(
		q.ctx,
		q.accountName,
		qTx.blobs,
		qTx.options...,
	)
	broadcastDuration := time.Since(broadcastStart)
	fmt.Printf("[TIMING] Broadcast call took %v\n", broadcastDuration)

	if err != nil || resp.Code != 0 {
		// Check if this is a sequence mismatch AND we're blocked
		// This means the sequence was rolled back while we were broadcasting
		// TODO: maybe we can check if q is blocked and if so, return
		// otherwise it could mean client is stalled
		if IsSequenceMismatchError(err) {
			fmt.Println("Sequence mismatch error in broadcast: ", err)
			// 	fmt.Println("Sequence mismatch error")
			// 	// check expected sequence and check if there is transaction with that sequence
			// 	expectedSeq := parseExpectedSequence(err.Error())
			// 	// check if there is transaction with that sequence
			// 	for _, txx := range q.queue {
			// 		fmt.Println("expectedSeq: ", expectedSeq)
			// 		if txx.sequence == expectedSeq {
			// 			fmt.Printf("Found transaction with expected sequence with hash %s\n", txx.txHash[:16])
			// 			// check status of tx
			// 			txClient := tx.NewTxClient(q.client.GetGRPCConnection())
			// 			statusResp, err := txClient.TxStatus(q.ctx, &tx.TxStatusRequest{TxId: txx.txHash})
			// 			if err != nil {
			// 				fmt.Printf("Failed to check status of tx %s: %v\n", txx.txHash[:16], err)
			// 				continue
			// 			}
			// 			if statusResp.Status == core.TxStatusRejected {
			// 				q.handleRejected(txx, statusResp, txClient)
			// 			}
			// 			fmt.Println("status for this expected hash: ", statusResp.Status)
			// 			fmt.Println("status log: ", statusResp.Error)
			// 			return
			// 		}

			// 	}
			// return because we are probably blocked, we will try again
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

	qTx.txHash = resp.TxHash
	qTx.txBytes = txBytes
	qTx.sequence = sequence - 1 // sequence is incremented after successful submission
	qTx.submittedAt = time.Now()

	fmt.Printf("Broadcast successful for tx %s - marking as broadcast in queue\n", qTx.txHash[:16])
	fmt.Printf("[TIMING] Total processNextTx took %v\n", time.Since(startTime))
}

// monitorLoop periodically checks the status of broadcast transactions
func (q *sequentialQueue) monitorLoop() {
	ticker := time.NewTicker(2 * time.Second)
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
	ticker := time.NewTicker(time.Second) //TODO: it's currently fine without additional delays. Might still be necessary tho.
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			return
		case <-q.ResignChan:
			// TODO: decide if we want to do anything during rejections
		case qTx := <-q.ResubmitChan:
			q.ResubmitEvicted(qTx)
		default:
			q.processNextTx()
		}
	}
}

// TODO: come back to this and see if it makes sense
// func (q *sequentialQueue) setTxInfo(qTx *queuedTx, resp *sdktypes.TxResponse, txBytes []byte, sequence uint64) {
//  q.mu.Lock()
//  defer q.mu.Unlock()

//  qTx.txHash = resp.TxHash
//  qTx.txBytes = txBytes
//  qTx.sequence = sequence
//  qTx.shouldResign = false
// }

func (q *sequentialQueue) ResubmitEvicted(qTx *queuedTx) {
	startTime := time.Now()
	fmt.Printf("Resubmitting evicted tx with hash %s and sequence %d\n", qTx.txHash[:16], qTx.sequence)
	q.mu.RLock()
	txBytes := qTx.txBytes
	q.mu.RUnlock()

	// check if the tx needs to be resubmitted
	resubmitStart := time.Now()
	resubmitResp, err := q.client.SendTxToConnection(q.ctx, q.client.GetGRPCConnection(), txBytes)
	resubmitDuration := time.Since(resubmitStart)
	fmt.Printf("[TIMING] Resubmit network call took %v\n", resubmitDuration)
	if err != nil || resubmitResp.Code != 0 {
		select {
		case qTx.resultsC <- SequentialSubmissionResult{
			Error: fmt.Errorf("evicted and failed to resubmit with hash %s: %w", qTx.txHash[:16], err),
		}:
		case <-q.ctx.Done():
		}
		// send error and remove from queue
		q.removeFromQueue(qTx)
		return
	}

	// Successful resubmission - reset flag and track metrics
	q.mu.Lock()
	qTx.isResubmitting = false
	q.resubmitCount++
	q.mu.Unlock()

	// Exit recovery mode after successful resubmission to allow new txs to be broadcast
	// q.setRecovering(false)

	fmt.Printf("Successfully resubmitted tx %s\n", qTx.txHash[:16])
	fmt.Printf("[TIMING] Total ResubmitEvicted took %v\n", time.Since(startTime))
}

// checkBroadcastTransactions checks status of all broadcast transactions
func (q *sequentialQueue) checkBroadcastTransactions() {
	startTime := time.Now()
	fmt.Println("Checking broadcast transactions")

	scanStart := time.Now()
	q.mu.RLock()
	// Collect broadcast transaction hashes (cap at 20 per batch for efficiency)
	const maxBatchSize = 20
	var broadcastTxHashes []string
	var broadcastTxs []*queuedTx
	for _, tx := range q.queue {
		if tx.txHash != "" {
			broadcastTxHashes = append(broadcastTxHashes, tx.txHash)
			broadcastTxs = append(broadcastTxs, tx)
			// Cap at 20 transactions per status check batch
			if len(broadcastTxHashes) >= maxBatchSize {
				break
			}
		}
	}
	fmt.Printf("Broadcast txs: %d\n", len(broadcastTxHashes))
	totalQueueSize := len(q.queue)
	q.mu.RUnlock()
	scanDuration := time.Since(scanStart)

	fmt.Printf("Total queue size: %d, Broadcast txs: %d\n", totalQueueSize, len(broadcastTxHashes))
	fmt.Printf("[TIMING] Collecting broadcast txs scan took %v\n", scanDuration)

	if len(broadcastTxHashes) == 0 {
		return
	}

	// Create tx client for status queries
	txClient := tx.NewTxClient(q.client.GetGRPCConnection())

	statusCheckStart := time.Now()

	// Try batch status check first
	statusResp, err := txClient.TxStatusBatch(q.ctx, &tx.TxStatusBatchRequest{TxIds: broadcastTxHashes})

	// If batch is not supported, fall back to individual status checks
	if err != nil {
		return
	}

	statusCheckCount := len(statusResp.Statuses)
	for i, statusRespp := range statusResp.Statuses {
		q.mu.RLock()
		qTx := broadcastTxs[i]
		q.mu.RUnlock()
		switch statusRespp.Status.Status {
		case core.TxStatusCommitted:
			q.handleCommitted(broadcastTxs[i], statusRespp.Status)
		case core.TxStatusEvicted:
			q.setRecovering(true)
			// Found an evicted tx - check if already being resubmitted to avoid duplicates
			q.mu.Lock()
			if qTx.isResubmitting {
				q.mu.Unlock()
				fmt.Printf("Tx %s is already being resubmitted - skipping\n", qTx.txHash[:16])
				continue
			}
			qTx.isResubmitting = true
			sequence := qTx.sequence
			q.mu.Unlock()
			fmt.Printf("Detected evicted tx with sequence %d - resubmitting\n", sequence)

			q.ResubmitChan <- qTx
		case core.TxStatusRejected:
			q.setRecovering(true)
			prevStatus := ""
			if i > 0 {
				prevStatus = statusResp.Statuses[i-1].Status.Status
			}
			q.handleRejected(qTx, statusRespp.Status, txClient, prevStatus)

			q.removeFromQueue(qTx)
			// return errror
			select {
			case qTx.resultsC <- SequentialSubmissionResult{
				Error: fmt.Errorf("tx rejected with code %d: %s", statusRespp.Status.ExecutionCode, statusRespp.Status.Error),
			}:
			case <-q.ctx.Done():
			}
		}
	}
	q.recomputeRecoveryState(statusResp.Statuses)

	statusCheckDuration := time.Since(statusCheckStart)
	if statusCheckCount > 0 {
		fmt.Printf("[TIMING] Status checks took %v for %d txs (avg: %v per tx)\n",
			statusCheckDuration, statusCheckCount, statusCheckDuration/time.Duration(statusCheckCount))
	}
	fmt.Printf("[TIMING] Total checkBroadcastTransactions took %v\n", time.Since(startTime))
}

func (q *sequentialQueue) recomputeRecoveryState(statuses []*tx.TxStatusResult) {
	q.mu.Lock()
	defer q.mu.Unlock()

	inRecovery := false

	for _, st := range statuses {
		switch st.Status.Status {
		case core.TxStatusRejected, core.TxStatusEvicted:
			// still bad
			inRecovery = true
		}
	}

	if inRecovery {
		if !q.isRecovering.Load() {
			fmt.Println("Entering recovery mode")
		}
		q.isRecovering.Store(true)
	} else {
		if q.isRecovering.Load() {
			fmt.Println("Exiting recovery mode")
		}
		q.isRecovering.Store(false)
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

	q.mu.RLock()
	fmt.Printf("LAST CONFIRMED SEQUENCE and HASH: %d, %s\n", q.lastConfirmedSeq, qTx.txHash[:16])
	q.mu.RUnlock()

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
func (q *sequentialQueue) handleRejected(qTx *queuedTx, statusResp *tx.TxStatusResponse, txClient tx.TxClient, prevStatus string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	rejectedSeq := qTx.sequence
	fmt.Printf("Detected rejected tx with sequence %d\n", rejectedSeq)

	// Get current sequence to check if we've already rolled back
	currentSeq := q.client.Signer().Account(q.accountName).Sequence()

	// If current sequence is already <= rejected sequence, we've already rolled back
	// (either from a previous rejection or sequence is already correct)
	// Since rejections are processed in order, if currentSeq > rejectedSeq, we can roll back directly
	// if currentSeq <= rejectedSeq {
	// 	fmt.Printf("Current sequence (%d) is already <= rejected sequence (%d) - skipping rollback\n", currentSeq, rejectedSeq)
	// 	return
	// }
	if prevStatus == core.TxStatusRejected || rejectedSeq-1 == q.lastRejectedSeq {
		fmt.Printf("Previous tx was rejected or current sequence is already the last rejected sequence - skipping rollback\n")
		return
	}

	if rejectedSeq > currentSeq {
		fmt.Printf("Rejected sequence (%d) is greater than current sequence (%d) - skipping rollback\n", rejectedSeq, currentSeq)
		return
	}

	// Roll back to the rejected sequence
	// Since rejections are processed in order, if we get here, no previous rejection caused a rollback
	fmt.Printf("Rolling back nonce from sequence %d to sequence %d\n", currentSeq, rejectedSeq)
	q.client.Signer().SetSequence(q.accountName, rejectedSeq)
	fmt.Printf("Rolled back signer sequence to %d\n", rejectedSeq)
	q.lastRejectedSeq = rejectedSeq
}

// removeFromQueue removes a transaction from the queue and frees its memory
func (q *sequentialQueue) removeFromQueue(qTx *queuedTx) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, tx := range q.queue {
		if tx == qTx {
			// Decrement memory counter
			blobsMemory := calculateBlobsMemory(qTx.blobs)
			if q.queueMemoryBytes >= blobsMemory {
				q.queueMemoryBytes -= blobsMemory
			} else {
				// Safety check - should never happen
				fmt.Printf("[WARNING] Memory accounting error: queueMemory=%d < blobsMemory=%d\n",
					q.queueMemoryBytes, blobsMemory)
				q.queueMemoryBytes = 0
			}

			// Remove from queue
			q.queue = append(q.queue[:i], q.queue[i+1:]...)
			return
		}
	}
}

// // isPreviousTxConfirmed checks if the previous transaction was confirmed
// func (q *sequentialQueue) isPreviousTxConfirmed(seq uint64) bool {
// 	q.mu.RLock()
// 	defer q.mu.RUnlock()
// 	if seq == 0 {
// 		return true
// 	}
// 	return q.lastConfirmedSeq >= seq-1
// }

// // isPreviousTxCommittedOrPending checks if the previous transaction is COMMITTED, PENDING, or EVICTED (not REJECTED)
// // Returns true if previous tx is committed/pending/evicted, false if rejected or can't determine
// func (q *sequentialQueue) isPreviousTxCommittedOrPending(seq uint64, txClient tx.TxClient) bool {
// 	// If sequence is 0, there's no previous transaction - this case is handled in handleRejected
// 	// But if we're called with seq 0, return true to allow rollback
// 	if seq == 0 {
// 		return true
// 	}
// 	prevSeq := seq - 1

// 	// First check if it's confirmed via lastConfirmedSeq
// 	q.mu.RLock()
// 	if q.lastConfirmedSeq >= prevSeq {
// 		q.mu.RUnlock()
// 		fmt.Printf("Previous tx (seq %d) is confirmed via lastConfirmedSeq (%d)\n", prevSeq, q.lastConfirmedSeq)
// 		return true
// 	}

// 	// Find the previous transaction in the queue
// 	var prevTx *queuedTx
// 	for _, txx := range q.queue {
// 		if txx.sequence == prevSeq && txx.txHash != "" {
// 			prevTx = txx
// 			break
// 		}
// 	}
// 	q.mu.RUnlock()

// 	if prevTx == nil {
// 		// Previous transaction not in queue - assume it's confirmed
// 		fmt.Printf("Previous tx (seq %d) not in queue - assuming confirmed\n", prevSeq)
// 		return true
// 	}

// 	// Check the actual status of the previous transaction
// 	statusResp, err := txClient.TxStatus(q.ctx, &tx.TxStatusRequest{TxId: prevTx.txHash})
// 	if err != nil {
// 		// If we can't check status, assume it's confirmed (safe default)
// 		fmt.Printf("Failed to check status of previous tx (seq %d): %v - assuming confirmed\n", prevSeq, err)
// 		return true
// 	}
// 	fmt.Printf("Previous tx (seq %d) status: %s\n", prevSeq, statusResp.Status)

// 	// Return true if COMMITTED, PENDING, or EVICTED (not REJECTED)
// 	// We roll back if previous was not rejected
// 	return statusResp.Status != core.TxStatusRejected
// }

// // isSequenceMismatchRejection checks if an error message indicates sequence mismatch
// func isSequenceMismatchRejection(errMsg string) bool {
// 	return strings.Contains(errMsg, "account sequence mismatch") ||
// 		strings.Contains(errMsg, "incorrect account sequence")
// }

// parseExpectedSequence extracts the expected sequence number from error message
// e.g., "account sequence mismatch, expected 9727, got 9811" -> returns 9727
func parseExpectedSequence(errMsg string) uint64 {
	// Look for "expected <number>"
	re := regexp.MustCompile(`expected (\d+)`)
	matches := re.FindStringSubmatch(errMsg)
	if len(matches) >= 2 {
		if seq, err := strconv.ParseUint(matches[1], 10, 64); err == nil {
			return seq
		}
	}
	return 0
}
