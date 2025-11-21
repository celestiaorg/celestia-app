package user

import (
	"sync"
	"time"
)

// TxTracker tracks transaction metadata for submitted transactions.
// It stores the hash, sequence, signer, and raw bytes for each transaction.
// This is used for:
// - Resubmitting evicted transactions with the same bytes
// - Rolling back sequence numbers on rejection
// - Pruning old transactions to prevent memory leaks
type TxTracker struct {
	mu      sync.RWMutex
	TxQueue map[string]*txInfo
}

// txInfo contains metadata about a submitted transaction
type txInfo struct {
	signer    string
	sequence  uint64
	txBytes   []byte
	timestamp time.Time
}

// NewTxTracker creates a new TxTracker instance
func NewTxTracker() *TxTracker {
	return &TxTracker{
		TxQueue: make(map[string]*txInfo),
	}
}

// trackTransaction adds a transaction to the tracker
func (t *TxTracker) trackTransaction(signer string, sequence uint64, txHash string, txBytes []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.TxQueue[txHash] = &txInfo{
		signer:    signer,
		sequence:  sequence,
		txBytes:   txBytes,
		timestamp: time.Now(),
	}
}

// GetTxFromTxTracker retrieves transaction metadata by hash
// Returns: sequence, signer, txBytes, exists
func (t *TxTracker) GetTxFromTxTracker(txHash string) (uint64, string, []byte, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	info, exists := t.TxQueue[txHash]
	if !exists {
		return 0, "", nil, false
	}
	
	return info.sequence, info.signer, info.txBytes, true
}

// RemoveTxFromTxTracker removes a transaction from the tracker
func (t *TxTracker) RemoveTxFromTxTracker(txHash string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.TxQueue, txHash)
}

// deleteFromTxTracker removes a transaction from the tracker (alias for RemoveTxFromTxTracker)
func (t *TxTracker) deleteFromTxTracker(txHash string) {
	t.RemoveTxFromTxTracker(txHash)
}

// GetTxBytes retrieves the transaction bytes for a given account and sequence
// Returns nil if not found
func (t *TxTracker) GetTxBytes(accountName string, sequence uint64) []byte {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Search through all transactions to find one matching the account and sequence
	for _, info := range t.TxQueue {
		if info.signer == accountName && info.sequence == sequence {
			return info.txBytes
		}
	}

	return nil
}

// pruneTxTracker removes transactions older than txTrackerPruningInterval
// This prevents memory leaks from accumulating transaction history
func (t *TxTracker) pruneTxTracker() {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoffTime := time.Now().Add(-txTrackerPruningInterval)

	for txHash, info := range t.TxQueue {
		if info.timestamp.Before(cutoffTime) {
			delete(t.TxQueue, txHash)
		}
	}
}

