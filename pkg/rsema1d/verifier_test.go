package rsema1d

import (
	"bytes"
	"math/rand/v2"
	"testing"
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

		root, err := v.Verify(commitment, rlcOrig, proofs)
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
	if _, err := v.Verify(commitment, rlcOrig, cleanProofs); err != nil {
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
	if _, err := v.Verify(commitment, rlcOrig, tampered); err == nil {
		t.Fatalf("tampered row was accepted")
	}

	// And the Verifier must remain usable after a failure.
	if _, err := v.Verify(commitment, rlcOrig, cleanProofs); err != nil {
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
		if _, err := v.Verify(commitment, rlcOrig, proofs); err != nil {
			t.Fatalf("n=%d: Verify: %v", n, err)
		}
	}
}
