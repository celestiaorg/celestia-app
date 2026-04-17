package v3

import (
	"fmt"
)

// txEntry represents a signed transaction in the ordered buffer.
type txEntry struct {
	sequence  uint64
	txHash    string
	txBytes   []byte
	request   *TxRequest
	submitted bool // true if sent to the node
}

// TxBuffer maintains an ordered sequence buffer for the async pipeline.
// All methods are NOT thread-safe; the single worker goroutine owns this.
//
// Key invariant: signed entries are contiguous:
//
//	signed[i].sequence == confirmed + 1 + i
type TxBuffer struct {
	confirmed uint64       // last confirmed sequence number
	nextSeq   uint64       // next sequence number to assign when signing
	pending   []*TxRequest // unsigned FIFO queue
	signed    []txEntry    // signed entries, ordered by sequence, no gaps
}

// NewTxBuffer creates a new TxBuffer starting at the given sequence number.
func NewTxBuffer(startSeq uint64) *TxBuffer {
	return &TxBuffer{
		confirmed: startSeq - 1, // nothing confirmed yet
		nextSeq:   startSeq,
		pending:   make([]*TxRequest, 0),
		signed:    make([]txEntry, 0),
	}
}

// AddPending adds a new unsigned transaction request to the pending queue.
func (b *TxBuffer) AddPending(req *TxRequest) {
	b.pending = append(b.pending, req)
}

// PendingLen returns the number of pending (unsigned) requests.
func (b *TxBuffer) PendingLen() int {
	return len(b.pending)
}

// PopPending removes and returns the next pending request.
// Returns nil if the pending queue is empty.
func (b *TxBuffer) PopPending() *TxRequest {
	if len(b.pending) == 0 {
		return nil
	}
	req := b.pending[0]
	b.pending = b.pending[1:]
	return req
}

// NextSequence returns the next sequence number to use for signing and
// advances the internal counter.
func (b *TxBuffer) NextSequence() uint64 {
	seq := b.nextSeq
	b.nextSeq++
	return seq
}

// AppendSigned appends a signed entry to the buffer, enforcing sequence continuity.
func (b *TxBuffer) AppendSigned(entry txEntry) error {
	expectedSeq := b.confirmed + 1 + uint64(len(b.signed))
	if entry.sequence != expectedSeq {
		return fmt.Errorf("sequence gap: expected %d, got %d", expectedSeq, entry.sequence)
	}
	b.signed = append(b.signed, entry)
	return nil
}

// SignedLen returns the number of signed entries in the buffer.
func (b *TxBuffer) SignedLen() int {
	return len(b.signed)
}

// Front returns the first signed entry without removing it.
// Returns nil if there are no signed entries.
func (b *TxBuffer) Front() *txEntry {
	if len(b.signed) == 0 {
		return nil
	}
	return &b.signed[0]
}

// ConfirmFront removes and returns the front entry, advancing the confirmed counter.
// Returns nil if the buffer is empty.
func (b *TxBuffer) ConfirmFront() *txEntry {
	if len(b.signed) == 0 {
		return nil
	}
	entry := b.signed[0]
	b.signed = b.signed[1:]
	b.confirmed = entry.sequence
	return &entry
}

// GetByHash finds a signed entry by its tx hash.
// Returns nil if not found.
func (b *TxBuffer) GetByHash(txHash string) *txEntry {
	for i := range b.signed {
		if b.signed[i].txHash == txHash {
			return &b.signed[i]
		}
	}
	return nil
}

// GetBySequence finds a signed entry by its sequence number.
// Returns nil if not found.
func (b *TxBuffer) GetBySequence(seq uint64) *txEntry {
	if len(b.signed) == 0 {
		return nil
	}
	firstSeq := b.signed[0].sequence
	if seq < firstSeq {
		return nil
	}
	idx := int(seq - firstSeq)
	if idx >= len(b.signed) {
		return nil
	}
	return &b.signed[idx]
}

// EntriesInRange returns signed entries with sequences in [from, to] inclusive.
func (b *TxBuffer) EntriesInRange(from, to uint64) []txEntry {
	if len(b.signed) == 0 || from > to {
		return nil
	}
	firstSeq := b.signed[0].sequence
	lastSeq := b.signed[len(b.signed)-1].sequence

	if from > lastSeq || to < firstSeq {
		return nil
	}

	startIdx := 0
	if from > firstSeq {
		startIdx = int(from - firstSeq)
	}
	endIdx := len(b.signed)
	if to < lastSeq {
		endIdx = int(to-firstSeq) + 1
	}

	result := make([]txEntry, endIdx-startIdx)
	copy(result, b.signed[startIdx:endIdx])
	return result
}

// RollbackTo removes all entries with sequence >= seq from the signed buffer.
// Returns the removed entries for error callbacks.
// Also resets nextSeq to seq so new signings start from there.
func (b *TxBuffer) RollbackTo(seq uint64) []txEntry {
	if len(b.signed) == 0 {
		return nil
	}
	firstSeq := b.signed[0].sequence
	if seq < firstSeq {
		// Rollback everything
		removed := make([]txEntry, len(b.signed))
		copy(removed, b.signed)
		b.signed = b.signed[:0]
		b.nextSeq = seq
		return removed
	}

	idx := int(seq - firstSeq)
	if idx >= len(b.signed) {
		return nil
	}

	removed := make([]txEntry, len(b.signed)-idx)
	copy(removed, b.signed[idx:])
	b.signed = b.signed[:idx]
	b.nextSeq = seq
	return removed
}

// Next returns the first signed entry that has not been submitted.
// Returns nil if all signed entries have been submitted or buffer is empty.
func (b *TxBuffer) Next() *txEntry {
	for i := range b.signed {
		if !b.signed[i].submitted {
			return &b.signed[i]
		}
	}
	return nil
}

// Reset marks all entries with sequence >= seq as not submitted.
// This is used when a sequence mismatch returns a lower expected sequence.
func (b *TxBuffer) Reset(seq uint64) {
	for i := range b.signed {
		if b.signed[i].sequence >= seq {
			b.signed[i].submitted = false
		}
	}
}

// LastSubmittedSeq returns the highest sequence that has been submitted.
// Returns 0 if nothing has been submitted.
func (b *TxBuffer) LastSubmittedSeq() uint64 {
	var last uint64
	for i := range b.signed {
		if b.signed[i].submitted && b.signed[i].sequence > last {
			last = b.signed[i].sequence
		}
	}
	return last
}

// LastConfirmed returns the last confirmed sequence number.
func (b *TxBuffer) LastConfirmed() uint64 {
	return b.confirmed
}
