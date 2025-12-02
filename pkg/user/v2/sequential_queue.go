package v2

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
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
	defaultSequentialQueueSize = 50 // Initial capacity for queue slice
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
		queue:            make([]*queuedTx, 0, defaultSequentialQueueSize),
		ResubmitChan:     make(chan *queuedTx, 10), // Buffered channel for resubmit requests (large to prevent blocking)
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

// submitJob adds a new transaction to the queue
func (q *sequentialQueue) submitJob(job *SequentialSubmissionJob) {
	// Wait for space in queue (backpressure)
	for {
		q.mu.Lock()
		if len(q.queue) < defaultSequentialQueueSize {
			// Space available - add transaction
			qTx := &queuedTx{
				blobs:    job.Blobs,
				options:  job.Options,
				resultsC: job.ResultsC,
			}
			q.queue = append(q.queue, qTx)
			q.mu.Unlock()
			return
		}

		// Queue full - unlock and wait
		q.mu.Unlock()

		select {
		case <-time.After(100 * time.Millisecond):
			// Wait a bit then retry
		case <-q.ctx.Done():
			// Context cancelled, exit
			return
		}
	}
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

	// Find first unbroadcast transaction (txHash is empty)
	// fmt.Println("Processing next tx")

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
		// TODO: ma\ybe we can check if q is blocked and if so, return
		// otherwise it could mean client is stalled
		if IsSequenceMismatchError(err) {
			fmt.Println("Sequence mismatch error")
			// check expected sequence and check if there is transaction with that sequence
			expectedSeq := parseExpectedSequence(err.Error())
			// check if there is transaction with that sequence
			for _, txx := range q.queue {
				fmt.Println("expectedSeq: ", expectedSeq)
				if txx.sequence == expectedSeq {
					fmt.Printf("Found transaction with expected sequence with hash %s\n", txx.txHash[:16])
					// check status of tx
					txClient := tx.NewTxClient(q.client.GetGRPCConnection())
					statusResp, err := txClient.TxStatus(q.ctx, &tx.TxStatusRequest{TxId: txx.txHash})
					if err != nil {
						fmt.Printf("Failed to check status of tx %s: %v\n", txx.txHash[:16], err)
						continue
					}
					if statusResp.Status == core.TxStatusRejected {
						q.handleRejected(txx, statusResp, txClient)
					}
					fmt.Println("status for this expected hash: ", statusResp.Status)
					fmt.Println("status log: ", statusResp.Error)
					return
				}

			}
			// No transaction found with expected sequence - return
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
		case <-ticker.C:
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
	fmt.Printf("Successfully resubmitted tx %s\n", qTx.txHash[:16])
	fmt.Printf("[TIMING] Total ResubmitEvicted took %v\n", time.Since(startTime))
}

// checkBroadcastTransactions checks status of all broadcast transactions
func (q *sequentialQueue) checkBroadcastTransactions() {
	startTime := time.Now()
	fmt.Println("Checking broadcast transactions")

	scanStart := time.Now()
	q.mu.RLock()
	// Collect all broadcast transactions (those with non-empty txHash)
	var broadcastTxs []*queuedTx
	for _, tx := range q.queue {
		if tx.txHash != "" {
			broadcastTxs = append(broadcastTxs, tx)
		}
	}
	fmt.Printf("Broadcast txs: %d\n", len(broadcastTxs))
	totalQueueSize := len(q.queue)
	q.mu.RUnlock()
	scanDuration := time.Since(scanStart)

	fmt.Printf("Total queue size: %d, Broadcast txs: %d\n", totalQueueSize, len(broadcastTxs))
	fmt.Printf("[TIMING] Collecting broadcast txs scan took %v\n", scanDuration)

	if len(broadcastTxs) == 0 {
		return
	}

	// Create tx client for status queries
	txClient := tx.NewTxClient(q.client.GetGRPCConnection())

	statusCheckStart := time.Now()
	statusCheckCount := 0
	for _, qTx := range broadcastTxs {
		statusCheckCount++
		statusResp, err := txClient.TxStatus(q.ctx, &tx.TxStatusRequest{TxId: qTx.txHash})
		if err != nil {
			qTx.resultsC <- SequentialSubmissionResult{
				Error: fmt.Errorf("tx status check failed: %w", err),
			}
		}

		fmt.Printf("Tx %s status: %s\n", qTx.txHash[:16], statusResp.Status)

		switch statusResp.Status {
		case core.TxStatusCommitted:
			q.handleCommitted(qTx, statusResp)
		case core.TxStatusEvicted:
			// Found an evicted tx - scan entire queue from beginning to find all evicted txs
			fmt.Printf("Detected evicted tx with sequence %d - scanning queue for all evictions", qTx.sequence)
			// check if the tx is already being resubmitted
			q.mu.RLock()
			alreadyResubmitting := qTx.isResubmitting
			q.mu.RUnlock()
			if alreadyResubmitting {
				fmt.Printf("Tx %s is already being resubmitted - skipping\n", qTx.txHash[:16])
				continue
			}
			q.mu.RLock()
			var potentialEvictions []*queuedTx
			for _, tx := range q.queue {
				if tx.txHash != "" && !tx.isResubmitting {
					potentialEvictions = append(potentialEvictions, tx)
				}
			}
			q.mu.RUnlock()

			// Check status of each transaction in order to find first evicted one since we might have received evictions while
			// already processing the queue
			// Collect ALL evicted transactions first
			var evictedTxs []*queuedTx
			for _, evictedTx := range potentialEvictions {
				statusResp, err := txClient.TxStatus(q.ctx, &tx.TxStatusRequest{TxId: evictedTx.txHash})
				if err != nil {
					continue
				}
				if statusResp.Status == core.TxStatusEvicted {
					evictedTxs = append(evictedTxs, evictedTx)
				}
			}

			// Now send them in order with proper locking
			for _, evictedTx := range evictedTxs {
				q.mu.Lock()
				if !evictedTx.isResubmitting {
					evictedTx.isResubmitting = true
					q.mu.Unlock()
					fmt.Printf("Sending evicted tx (seq %d) to resubmit channel\n", evictedTx.sequence)
					q.ResubmitChan <- evictedTx
				} else {
					q.mu.Unlock()
					fmt.Printf("Skipping evicted tx (seq %d) - already being resubmitted\n", evictedTx.sequence)
				}
			}
			return // Skip processing remaining txs in this poll cycle
		case core.TxStatusRejected:
			q.handleRejected(qTx, statusResp, txClient)
		}
	}

	statusCheckDuration := time.Since(statusCheckStart)
	fmt.Printf("[TIMING] Status checks took %v for %d txs (avg: %v per tx)\n",
		statusCheckDuration, statusCheckCount, statusCheckDuration/time.Duration(statusCheckCount))
	fmt.Printf("[TIMING] Total checkBroadcastTransactions took %v\n", time.Since(startTime))
}

func (q *sequentialQueue) handleEvicted(qTx *queuedTx, statusResp *tx.TxStatusResponse, txClient tx.TxClient) {

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
func (q *sequentialQueue) handleRejected(qTx *queuedTx, statusResp *tx.TxStatusResponse, txClient tx.TxClient) {
	// Nonce/sequence mismatch - scan entire queue from beginning to find all rejected txs
	fmt.Printf("Detected rejected tx with sequence %d - scanning queue for all rejections\n", qTx.sequence)

	// Step 2: Collect all broadcast transactions to check (including those already marked for resignation)
	q.mu.RLock()
	var allBroadcastTxs []*queuedTx
	for _, tx := range q.queue {
		if tx.txHash != "" {
			allBroadcastTxs = append(allBroadcastTxs, tx)
		}
	}
	q.mu.RUnlock()

	// Step 3: Check status of each transaction to find all rejected ones
	var rejectedTxs []*queuedTx
	for _, qTx := range allBroadcastTxs {
		statusResp, err := txClient.TxStatus(q.ctx, &tx.TxStatusRequest{TxId: qTx.txHash})
		if err != nil {
			continue
		}
		if statusResp.Status == core.TxStatusRejected && statusResp.ExecutionCode == 32 {
			rejectedTxs = append(rejectedTxs, qTx)
		}
	}

	// Step 3a: Find the earliest rejected tx and roll back sequence if needed
	if len(rejectedTxs) > 0 {
		// Find the earliest rejected tx (lowest sequence)
		earliestRejected := rejectedTxs[0]

		// Check if the transaction before the earliest rejected one was confirmed or pending
		fmt.Println("EARLIEST REJECTED TX SEQUENCE: ", earliestRejected.sequence)
		if q.isPreviousTxCommittedOrPending(earliestRejected.sequence, txClient) {
			fmt.Println("FOR SEQUENCE MISMATCH REJECTIONS")
			fmt.Println("LAST CONFIRMED SEQUENCE: ", q.lastConfirmedSeq)
			fmt.Println("SEQUENCE TO ROLL BACK TO: ", earliestRejected.sequence)
			q.mu.Lock()
			q.client.Signer().SetSequence(q.accountName, q.lastConfirmedSeq+1)
			q.mu.Unlock()
			fmt.Printf("Rolled back signer sequence to %d (earliest rejected tx)\n", earliestRejected.sequence)
		}
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

// isPreviousTxCommittedOrPending checks if the previous transaction is COMMITTED or PENDING
func (q *sequentialQueue) isPreviousTxCommittedOrPending(seq uint64, txClient tx.TxClient) bool {
	if seq == 0 {
		return true
	}
	prevSeq := seq - 1

	// First check if it's confirmed via lastConfirmedSeq
	q.mu.RLock()
	if q.lastConfirmedSeq >= prevSeq {
		q.mu.RUnlock()
		return true
	}

	// Find the previous transaction in the queue
	var prevTx *queuedTx
	for _, txx := range q.queue {
		if txx.sequence == prevSeq && txx.txHash != "" {
			prevTx = txx
			break
		}
	}
	q.mu.RUnlock()

	if prevTx == nil {
		// Previous transaction not in queue - assume it's confirmed
		return true
	}

	// Check the actual status of the previous transaction
	statusResp, err := txClient.TxStatus(q.ctx, &tx.TxStatusRequest{TxId: prevTx.txHash})
	if err != nil {
		// If we can't check status, assume it's confirmed
		return true
	}
	fmt.Println("PREVIOUS TX STATUS Seq: ", prevSeq, " RESPONSE: ", statusResp.Status, "LOG: ", statusResp.Error)

	// Return true if COMMITTED or PENDING
	return statusResp.Status == core.TxStatusCommitted || statusResp.Status == core.TxStatusPending
}

// isSequenceMismatchRejection checks if an error message indicates sequence mismatch
func isSequenceMismatchRejection(errMsg string) bool {
	return strings.Contains(errMsg, "account sequence mismatch") ||
		strings.Contains(errMsg, "incorrect account sequence")
}

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
