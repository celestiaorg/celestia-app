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

// txBuffer maintains an ordered sequence buffer for the async pipeline.
// All methods are NOT thread-safe; the single worker goroutine owns this.
//
// Key invariant: signed entries are contiguous:
//
//	signed[i].sequence == confirmed + 1 + i
type txBuffer struct {
	confirmed uint64       // last confirmed sequence number
	pending   []*TxRequest // unsigned FIFO queue
	signed    []txEntry    // signed entries, ordered by sequence, no gaps
}

// newTxBuffer creates a new txBuffer starting at the given sequence number.
func newTxBuffer(startSeq uint64) *txBuffer {
	return &txBuffer{
		confirmed: startSeq - 1, // nothing confirmed yet
		pending:   make([]*TxRequest, 0),
		signed:    make([]txEntry, 0),
	}
}

// addPending adds a new unsigned transaction request to the pending queue.
func (b *txBuffer) addPending(req *TxRequest) {
	b.pending = append(b.pending, req)
}

// pendingLen returns the number of pending (unsigned) requests.
func (b *txBuffer) pendingLen() int {
	return len(b.pending)
}

// popPending removes and returns the next pending request.
// Returns nil if the pending queue is empty.
func (b *txBuffer) popPending() *TxRequest {
	if len(b.pending) == 0 {
		return nil
	}
	req := b.pending[0]
	b.pending = b.pending[1:]
	return req
}

// appendSigned appends a signed entry to the buffer, enforcing sequence continuity.
func (b *txBuffer) appendSigned(entry txEntry) error {
	expectedSeq := b.confirmed + 1 + uint64(len(b.signed))
	if entry.sequence != expectedSeq {
		return fmt.Errorf("sequence gap: expected %d, got %d", expectedSeq, entry.sequence)
	}
	b.signed = append(b.signed, entry)
	return nil
}

// signedLen returns the number of signed entries in the buffer.
func (b *txBuffer) signedLen() int {
	return len(b.signed)
}

// front returns the first signed entry without removing it.
// Returns nil if there are no signed entries.
func (b *txBuffer) front() *txEntry {
	if len(b.signed) == 0 {
		return nil
	}
	return &b.signed[0]
}

// confirmFront removes and returns the front entry, advancing the confirmed counter.
// Returns nil if the buffer is empty.
func (b *txBuffer) confirmFront() *txEntry {
	if len(b.signed) == 0 {
		return nil
	}
	entry := b.signed[0]
	b.signed = b.signed[1:]
	b.confirmed = entry.sequence
	return &entry
}

// getBySequence finds a signed entry by its sequence number.
// Returns nil if not found.
func (b *txBuffer) getBySequence(seq uint64) *txEntry {
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

// next returns the first signed entry that has not been submitted.
// Returns nil if all signed entries have been submitted or buffer is empty.
func (b *txBuffer) next() *txEntry {
	for i := range b.signed {
		if !b.signed[i].submitted {
			return &b.signed[i]
		}
	}
	return nil
}

// reset marks all entries with sequence >= seq as not submitted.
// This is used when a sequence mismatch returns a lower expected sequence.
func (b *txBuffer) reset(seq uint64) {
	for i := range b.signed {
		if b.signed[i].sequence >= seq {
			b.signed[i].submitted = false
		}
	}
}

// lastSubmittedSeq returns the highest sequence that has been submitted.
// Returns 0 if nothing has been submitted.
func (b *txBuffer) lastSubmittedSeq() uint64 {
	var last uint64
	for i := range b.signed {
		if b.signed[i].submitted && b.signed[i].sequence > last {
			last = b.signed[i].sequence
		}
	}
	return last
}

// lastConfirmed returns the last confirmed sequence number.
func (b *txBuffer) lastConfirmed() uint64 {
	return b.confirmed
}
