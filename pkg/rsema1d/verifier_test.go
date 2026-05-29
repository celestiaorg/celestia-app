package rsema1d

import (
	"bytes"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
)

// TestVerifierMatchesContextRoot cross-checks the rlcOrigRoot returned by
// Verify against the one CreateVerificationContext computes from the same
// rlcOrig — both build the same padded RLC tree and must agree.
func TestVerifierMatchesContextRoot(t *testing.T) {
	cfg := &Config{K: 64, N: 64, RowSize: 1024, WorkerCount: 2}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	r := rand.New(rand.NewPCG(42, 42))
	for shard := range 3 {
		data := make([][]byte, cfg.K)
		for i := range data {
			data[i] = make([]byte, cfg.RowSize)
			for j := range data[i] {
				data[i][j] = byte(r.IntN(256))
			}
		}
		ed, commitment, rlcOrig, err := Encode(data, cfg)
		if err != nil {
			t.Fatalf("shard %d: Encode: %v", shard, err)
		}

		proofs := make([]*RowProof, 16)
		for i := range proofs {
			p, err := ed.GenerateRowProof(i)
			if err != nil {
				t.Fatalf("shard %d: GenerateRowProof(%d): %v", shard, i, err)
			}
			proofs[i] = p
		}

		root, err := v.Verify(commitment, proofs, rlcOrig)
		if err != nil {
			t.Fatalf("shard %d: Verify: %v", shard, err)
		}

		// Cross-check the rlcOrigRoot against the existing API.
		_, expectedRoot, err := CreateVerificationContext(rlcOrig, cfg)
		if err != nil {
			t.Fatalf("shard %d: CreateVerificationContext: %v", shard, err)
		}
		if !bytes.Equal(root, expectedRoot[:]) {
			t.Fatalf("shard %d: rlcOrigRoot mismatch: got %x want %x", shard, root, expectedRoot)
		}
	}
}

// TestVerifierRejectsTamperedRow ensures buffer reuse across calls does not
// mask tampering: a corrupted row in any iteration must surface an error.
func TestVerifierRejectsTamperedRow(t *testing.T) {
	cfg := &Config{K: 64, N: 64, RowSize: 1024, WorkerCount: 2}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	r := rand.New(rand.NewPCG(7, 7))
	data := make([][]byte, cfg.K)
	for i := range data {
		data[i] = make([]byte, cfg.RowSize)
		for j := range data[i] {
			data[i][j] = byte(r.IntN(256))
		}
	}
	ed, commitment, rlcOrig, err := Encode(data, cfg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Run a clean verify first to populate the Verifier's internal buffers.
	cleanProofs := make([]*RowProof, 16)
	for i := range cleanProofs {
		p, err := ed.GenerateRowProof(i)
		if err != nil {
			t.Fatalf("GenerateRowProof(%d): %v", i, err)
		}
		cleanProofs[i] = p
	}
	if _, err := v.Verify(commitment, cleanProofs, rlcOrig); err != nil {
		t.Fatalf("clean verify: %v", err)
	}

	// Re-issue proofs with a tampered row and confirm the next Verify call
	// rejects the batch despite the buffers carrying state from the prior call.
	tampered := make([]*RowProof, 16)
	for i := range tampered {
		p, err := ed.GenerateRowProof(i)
		if err != nil {
			t.Fatalf("GenerateRowProof(%d): %v", i, err)
		}
		row := append([]byte(nil), p.Row...)
		tampered[i] = &RowProof{Index: p.Index, Row: row, RowProof: p.RowProof}
	}
	tampered[3].Row[0] ^= 0xFF
	if _, err := v.Verify(commitment, tampered, rlcOrig); err == nil {
		t.Fatalf("tampered row was accepted")
	}

	// And the Verifier must remain usable after a failure.
	if _, err := v.Verify(commitment, cleanProofs, rlcOrig); err != nil {
		t.Fatalf("post-failure verify: %v", err)
	}
}

// TestVerifierVariableBatchSize exercises the per-call grow buffers
// (rowRoots, rowsView) when batch size shrinks then grows.
func TestVerifierVariableBatchSize(t *testing.T) {
	cfg := &Config{K: 64, N: 64, RowSize: 1024, WorkerCount: 1}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	r := rand.New(rand.NewPCG(99, 99))
	data := make([][]byte, cfg.K)
	for i := range data {
		data[i] = make([]byte, cfg.RowSize)
		for j := range data[i] {
			data[i][j] = byte(r.IntN(256))
		}
	}
	ed, commitment, rlcOrig, err := Encode(data, cfg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	for _, n := range []int{32, 8, 48} {
		proofs := make([]*RowProof, n)
		for i := range proofs {
			p, err := ed.GenerateRowProof(i)
			if err != nil {
				t.Fatalf("n=%d: GenerateRowProof(%d): %v", n, i, err)
			}
			proofs[i] = p
		}
		if _, err := v.Verify(commitment, proofs, rlcOrig); err != nil {
			t.Fatalf("n=%d: Verify: %v", n, err)
		}
	}
}

// TestVerifierVerifyShared checks that VerifyShared reuses the RLC state cached
// by a prior Verify: it accepts an honest disjoint batch, rejects a tampered
// one, and requires Verify to have run first.
func TestVerifierVerifyShared(t *testing.T) {
	cfg := &Config{K: 64, N: 64, RowSize: 1024, WorkerCount: 2}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	r := rand.New(rand.NewPCG(11, 11))
	ed, commitment, rlcOrig := encodeRandom(t, r, cfg)

	// Without a prior Verify the cached RLC root is zero, so the shared
	// commitment check must fail rather than silently accept.
	if err := v.VerifyShared(commitment, rangeProofs(t, ed, 0, 16)); err == nil {
		t.Fatalf("VerifyShared before Verify was accepted")
	}

	// Verify primes the RS extension and RLC root for the shared calls.
	if _, err := v.Verify(commitment, rangeProofs(t, ed, 0, 16), rlcOrig); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// A disjoint honest batch verifies against the cached state.
	if err := v.VerifyShared(commitment, rangeProofs(t, ed, 16, 32)); err != nil {
		t.Fatalf("VerifyShared honest batch: %v", err)
	}

	// A tampered row in the shared batch is rejected.
	tampered := rangeProofs(t, ed, 32, 48)
	tampered[2].Row = append([]byte(nil), tampered[2].Row...)
	tampered[2].Row[0] ^= 0xFF
	if err := v.VerifyShared(commitment, tampered); err == nil {
		t.Fatalf("VerifyShared accepted tampered row")
	}

	// The verifier remains usable after a rejected batch.
	if err := v.VerifyShared(commitment, rangeProofs(t, ed, 48, 64)); err != nil {
		t.Fatalf("VerifyShared after rejection: %v", err)
	}
}

// TestVerifierVerifySharedConcurrent runs many VerifyShared calls against the
// same cached RLC state from parallel goroutines. Best run with -race, which is
// what guards the "concurrent-safe after Verify" contract.
func TestVerifierVerifySharedConcurrent(t *testing.T) {
	cfg := &Config{K: 64, N: 64, RowSize: 1024, WorkerCount: 2}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	r := rand.New(rand.NewPCG(23, 23))
	ed, commitment, rlcOrig := encodeRandom(t, r, cfg)

	// Prime the shared RLC state once; the goroutines below only call VerifyShared.
	if _, err := v.Verify(commitment, rangeProofs(t, ed, 0, 16), rlcOrig); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// Each goroutine owns a disjoint, independently built proof batch.
	const workers = 4
	batches := make([][]*RowProof, workers)
	for w := range batches {
		lo := w * 16
		batches[w] = rangeProofs(t, ed, lo, lo+16)
	}

	var wg sync.WaitGroup
	errs := make([]error, workers)
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for range 20 {
				if err := v.VerifyShared(commitment, batches[w]); err != nil {
					errs[w] = err
					return
				}
			}
		}(w)
	}
	wg.Wait()

	for w, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: VerifyShared: %v", w, err)
		}
	}
}

// encodeRandom encodes a fresh random K×RowSize matrix and returns the extended
// data alongside the commitment and original RLC vector.
func encodeRandom(t *testing.T, r *rand.Rand, cfg *Config) (*ExtendedData, Commitment, rlc.Vector) {
	t.Helper()
	data := make([][]byte, cfg.K)
	for i := range data {
		data[i] = make([]byte, cfg.RowSize)
		for j := range data[i] {
			data[i][j] = byte(r.IntN(256))
		}
	}
	ed, commitment, rlcOrig, err := Encode(data, cfg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return ed, commitment, rlcOrig
}

// rangeProofs generates row proofs for indices [lo, hi).
func rangeProofs(t *testing.T, ed *ExtendedData, lo, hi int) []*RowProof {
	t.Helper()
	proofs := make([]*RowProof, hi-lo)
	for i := range proofs {
		p, err := ed.GenerateRowProof(lo + i)
		if err != nil {
			t.Fatalf("GenerateRowProof(%d): %v", lo+i, err)
		}
		proofs[i] = p
	}
	return proofs
}
