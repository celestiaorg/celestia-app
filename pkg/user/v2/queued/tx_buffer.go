package queued

import (
	"fmt"
)

// txEntry represents a signed transaction in the ordered buffer.
type txEntry struct {
	sequence uint64
	txHash   string
	txBytes  []byte
	request  *TxRequest
}

// txBuffer maintains an ordered sequence buffer for the async pipeline.
// All methods are NOT thread-safe; the single worker goroutine owns this.
//
// Invariants:
//   - signed[i].sequence == nextSeq + i  (contiguous, no gaps)
//   - submittedThru is the count of entries already submitted; equivalently,
//     the sequence of the next entry to submit is nextSeq + submittedThru.
type txBuffer struct {
	nextSeq       uint64       // sequence the next appended signed entry must have
	submittedThru uint64       // index into signed[] of the next unsubmitted entry
	pending       []*TxRequest // unsigned FIFO queue
	signed        []txEntry    // signed entries, ordered by sequence, no gaps
}

// newTxBuffer creates a new txBuffer starting at the given sequence number.
// startSeq == 0 (brand-new account) is supported.
func newTxBuffer(startSeq uint64) *txBuffer {
	return &txBuffer{
		nextSeq: startSeq,
		pending: make([]*TxRequest, 0),
		signed:  make([]txEntry, 0),
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
func (b *txBuffer) popPending() *TxRequest {
	if len(b.pending) == 0 {
		return nil
	}
	req := b.pending[0]
	b.pending[0] = nil // drop ref so the backing array doesn't pin TxRequest
	b.pending = b.pending[1:]
	if len(b.pending) == 0 {
		b.pending = nil // release backing array during idle periods
	}
	return req
}

// nextExpectedSeq returns the sequence the next appended signed entry must
// have. The worker passes this into the signer so queued stays authoritative over
// sequence assignment regardless of any sequence resets the underlying v1
// gas-estimation path may perform.
func (b *txBuffer) nextExpectedSeq() uint64 {
	return b.nextSeq + uint64(len(b.signed))
}

// appendSigned appends a signed entry to the buffer, enforcing sequence continuity.
func (b *txBuffer) appendSigned(entry txEntry) error {
	expected := b.nextExpectedSeq()
	if entry.sequence != expected {
		return fmt.Errorf("sequence gap: expected %d, got %d", expected, entry.sequence)
	}
	b.signed = append(b.signed, entry)
	return nil
}

// signedLen returns the number of signed entries in the buffer.
func (b *txBuffer) signedLen() int {
	return len(b.signed)
}

// front returns the first signed entry without removing it.
func (b *txBuffer) front() *txEntry {
	if len(b.signed) == 0 {
		return nil
	}
	return &b.signed[0]
}

// confirmFront removes and returns the front entry, advancing nextSeq.
func (b *txBuffer) confirmFront() *txEntry {
	if len(b.signed) == 0 {
		return nil
	}
	entry := b.signed[0]
	// Drop references in the vacated slot so the GC can reclaim txBytes /
	// request — otherwise the backing array keeps them alive.
	b.signed[0] = txEntry{}
	b.signed = b.signed[1:]
	if len(b.signed) == 0 {
		b.signed = nil // release backing array during idle periods
	}
	b.nextSeq = entry.sequence + 1
	if b.submittedThru > 0 {
		b.submittedThru--
	}
	return &entry
}

// getBySequence finds a signed entry by its sequence number.
func (b *txBuffer) getBySequence(seq uint64) *txEntry {
	if len(b.signed) == 0 || seq < b.nextSeq {
		return nil
	}
	idx := seq - b.nextSeq
	if idx >= uint64(len(b.signed)) {
		return nil
	}
	return &b.signed[idx]
}

// next returns the next signed entry that has not been submitted, or nil
// if all signed entries are submitted.
func (b *txBuffer) next() *txEntry {
	if b.submittedThru >= uint64(len(b.signed)) {
		return nil
	}
	return &b.signed[b.submittedThru]
}

// markSubmitted records that the entry at seq has been submitted.
// No-op if seq is outside the current signed range or already counted.
func (b *txBuffer) markSubmitted(seq uint64) {
	if seq < b.nextSeq {
		return
	}
	idx := seq - b.nextSeq
	if idx >= uint64(len(b.signed)) {
		return
	}
	if idx+1 > b.submittedThru {
		b.submittedThru = idx + 1
	}
}

// reset marks all entries with sequence >= seq as not submitted.
func (b *txBuffer) reset(seq uint64) {
	if seq <= b.nextSeq {
		b.submittedThru = 0
		return
	}
	idx := seq - b.nextSeq
	if idx < b.submittedThru {
		b.submittedThru = idx
	}
}

// lastSubmittedSeq returns the sequence of the most recently submitted entry,
// or nextSeq-1 if nothing in the buffer has been submitted yet. Caller should
// not assume the returned value is a valid sequence when submittedThru == 0
// and nextSeq == 0 (cold start).
func (b *txBuffer) lastSubmittedSeq() uint64 {
	if b.submittedThru == 0 {
		return 0
	}
	return b.nextSeq + b.submittedThru - 1
}

// hasSubmissions reports whether any entry has been submitted.
func (b *txBuffer) hasSubmissions() bool {
	return b.submittedThru > 0
}
