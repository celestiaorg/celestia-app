package rsema1d

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
)

// ErrNotEnoughRows is returned when reconstruction is attempted before K
// unique rows have been added to a Reconstructor.
var ErrNotEnoughRows = errors.New("not enough rows to reconstruct")

// Reconstructor is the download-path row collector: it collects RLC-verified
// row proofs from many sources and recovers the K original rows once enough
// unique indices have arrived. Because every contributing row is RLC-verified
// against the target commitment as it lands, [Reconstructor.Reconstruct] runs
// only Reed-Solomon data-shard recovery and skips the merkle/commitment rebuild.
//
// Reconstructor does not own row storage. The caller supplies a row buffer to
// [Reconstructor.Reconstruct]; dedup happens internally via a per-index seen
// bitset.
//
// Concurrency:
//
//   - [Reconstructor.Add] is safe to call from many goroutines. The first
//     successful Add installs the cached RLC under verifierMu; subsequent Adds
//     take the lock-free fast path through VerifyShared. Dedup and the
//     have-count update run under seenMu.
//   - Concurrent Add calls return disjoint Index sets, so callers may store
//     the returned novel proofs into their row buffer without further
//     synchronization.
//   - [Reconstructor.Reconstruct] must not race with Add. Drain in-flight Adds
//     before calling it.
type Reconstructor struct {
	coder *Coder

	commitment Commitment

	verifierMu  sync.Mutex
	verifierRLC atomic.Bool
	verifier    *Verifier

	seenMu sync.Mutex
	seen   []bool
	have   int
}

// NewReconstructor builds a Reconstructor bound to this Coder's config and the
// target commitment.
func (c *Coder) NewReconstructor(commitment Commitment) (*Reconstructor, error) {
	verifier, err := NewVerifier(c.config)
	if err != nil {
		return nil, err
	}
	return &Reconstructor{
		coder:      c,
		commitment: commitment,
		verifier:   verifier,
		seen:       make([]bool, c.config.K+c.config.N),
	}, nil
}

// Add verifies a batch of row proofs against the commitment using the given
// RLC, marks newly-unique indices as seen, and returns the novel proofs (those
// whose Index had not been seen before).
//
// Dedup compacts in place: the returned slice shares the input's backing
// array — Add overwrites proofs[0:n] with the novel ones, where n is the
// returned length. Any caller-held reference to the original slice is
// invalidated; iterate the returned slice exclusively.
//
// Safe to call concurrently. Concurrent Add calls return disjoint Index sets,
// so callers can store novel proofs into their own buffer without locking.
func (r *Reconstructor) Add(proofs []*RowProof, rlc rlc.Vector) ([]*RowProof, error) {
	if err := r.verify(rlc, proofs); err != nil {
		return nil, err
	}
	return r.addVerified(proofs), nil
}

func (r *Reconstructor) addVerified(proofs []*RowProof) []*RowProof {
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	n := 0
	for _, p := range proofs {
		if r.seen[p.Index] {
			continue
		}
		r.seen[p.Index] = true
		proofs[n] = p
		n++
		r.have++
	}
	return proofs[:n]
}

// Have returns the number of unique rows seen so far.
func (r *Reconstructor) Have() int {
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	return r.have
}

// Want returns the number of additional unique rows needed before
// [Reconstructor.Reconstruct] can run. Returns 0 when ready.
func (r *Reconstructor) Want() int {
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	if r.have >= r.coder.config.K {
		return 0
	}
	return r.coder.config.K - r.have
}

// Reconstruct fills any missing original rows in place via Reed-Solomon
// data-shard recovery. rows must have length K+N, with previously-Added rows
// placed at their reported indices and missing entries set to nil. Caller
// must ensure no Add is in flight.
func (r *Reconstructor) Reconstruct(rows [][]byte) error {
	if want := r.Want(); want > 0 {
		return fmt.Errorf("%w: need %d more rows", ErrNotEnoughRows, want)
	}
	if err := r.coder.enc.ReconstructData(rows); err != nil {
		return fmt.Errorf("reconstructing original rows: %w", err)
	}
	return nil
}

// verify checks proofs against r.commitment. Fast path: one atomic load and a
// concurrent-safe VerifyShared against the cached RLC. Slow path: serialize on
// verifierMu and install this caller's RLC, but only mark it cached if Verify
// validates it against the commitment.
func (r *Reconstructor) verify(rlc rlc.Vector, proofs []*RowProof) error {
	if r.verifierRLC.Load() {
		return r.verifier.VerifyShared(r.commitment, proofs)
	}

	r.verifierMu.Lock()
	defer r.verifierMu.Unlock()
	if r.verifierRLC.Load() {
		// another caller installed a validated RLC while we waited for the lock
		return r.verifier.VerifyShared(r.commitment, proofs)
	}
	if _, err := r.verifier.Verify(r.commitment, proofs, rlc); err != nil {
		return err
	}
	r.verifierRLC.Store(true)
	return nil
}
