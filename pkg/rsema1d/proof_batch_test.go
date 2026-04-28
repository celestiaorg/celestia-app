package rsema1d

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// TestVerifyRowsWithContextMatchesScalar locks in that batched verify accepts
// valid row proofs that the scalar path accepts — across the K values that
// matter in production (≥ minBatchedVerifyK plus some small ones to exercise
// the scalar fallback).
func TestVerifyRowsWithContextMatchesScalar(t *testing.T) {
	for _, nProofs := range []int{1, 4, 8, 16, 32, 64} {
		t.Run("N="+itoa(nProofs), func(t *testing.T) {
			// Build a realistic extended data set: K=64 rows of 1024 bytes,
			// then sample nProofs rows out of the K+N total.
			cfg := &Config{K: 64, N: 64, RowSize: 1024, WorkerCount: 1}
			data := make([][]byte, cfg.K)
			r := rand.New(rand.NewPCG(uint64(nProofs), 1))
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

			ctxScalar, _, err := CreateVerificationContext(rlcOrig, cfg)
			if err != nil {
				t.Fatalf("CreateVerificationContext: %v", err)
			}
			ctxBatched, _, err := CreateVerificationContext(rlcOrig, cfg)
			if err != nil {
				t.Fatalf("CreateVerificationContext: %v", err)
			}

			proofs := make([]*RowProof, nProofs)
			for i := range proofs {
				// Pick distinct indices in [0, K+N).
				idx := (i * 3) % (cfg.K + cfg.N)
				p, err := ed.GenerateRowProof(idx)
				if err != nil {
					t.Fatalf("GenerateRowProof(%d): %v", idx, err)
				}
				proofs[i] = p
			}

			// Scalar loop reference.
			for _, p := range proofs {
				if err := VerifyRowWithContext(p, commitment, ctxScalar); err != nil {
					t.Fatalf("VerifyRowWithContext: %v", err)
				}
			}

			// Batched path.
			if err := VerifyRowsWithContext(proofs, commitment, ctxBatched); err != nil {
				t.Fatalf("VerifyRowsWithContext: %v", err)
			}
		})
	}
}

// TestVerifyRowsWithContextDetectsTamperedRow verifies that corrupting any row
// in a batch makes the batched verify return an error. Ensures SIMD path does
// not hide tampering.
func TestVerifyRowsWithContextDetectsTamperedRow(t *testing.T) {
	cfg := &Config{K: 64, N: 64, RowSize: 1024, WorkerCount: 1}
	data := make([][]byte, cfg.K)
	r := rand.New(rand.NewPCG(7, 7))
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

	for tamperedIdx := range 16 {
		ctx, _, err := CreateVerificationContext(rlcOrig, cfg)
		if err != nil {
			t.Fatalf("CreateVerificationContext: %v", err)
		}
		proofs := make([]*RowProof, 16)
		for i := range proofs {
			p, err := ed.GenerateRowProof(i)
			if err != nil {
				t.Fatalf("GenerateRowProof: %v", err)
			}
			// deep-copy Row to avoid mutating ed state
			rowCopy := append([]byte(nil), p.Row...)
			p.Row = rowCopy
			proofs[i] = p
		}
		// Tamper with one byte of one row.
		proofs[tamperedIdx].Row[0] ^= 0xFF
		if err := VerifyRowsWithContext(proofs, commitment, ctx); err == nil {
			t.Fatalf("tampered row %d was accepted", tamperedIdx)
		}
	}
}

// TestVerifyRowsWithContextNilProof covers the edge case where a caller passes
// a nil proof element. The nil-first case is specifically tested with a
// context whose config.RowSize == 0 — that path dereferences proofs[0] to
// derive the expected row size, and would panic without the nil pre-check.
func TestVerifyRowsWithContextNilProof(t *testing.T) {
	encodeCfg := &Config{K: 64, N: 64, RowSize: 1024, WorkerCount: 1}
	data := make([][]byte, encodeCfg.K)
	r := rand.New(rand.NewPCG(11, 11))
	for i := range data {
		data[i] = make([]byte, encodeCfg.RowSize)
		for j := range data[i] {
			data[i][j] = byte(r.IntN(256))
		}
	}
	ed, commitment, rlcOrig, err := Encode(data, encodeCfg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Verify context with RowSize==0 exercises the proofs[0].Row dereference
	// path inside VerifyRowsWithContext.
	verifyCfg := &Config{K: encodeCfg.K, N: encodeCfg.N, WorkerCount: 1}

	for _, nilPos := range []int{0, minBatchedVerifyK / 2, minBatchedVerifyK} {
		ctx, _, err := CreateVerificationContext(rlcOrig, verifyCfg)
		if err != nil {
			t.Fatalf("CreateVerificationContext: %v", err)
		}
		proofs := make([]*RowProof, minBatchedVerifyK+4)
		for i := range proofs {
			p, err := ed.GenerateRowProof(i)
			if err != nil {
				t.Fatalf("GenerateRowProof: %v", err)
			}
			proofs[i] = p
		}
		proofs[nilPos] = nil
		if err := VerifyRowsWithContext(proofs, commitment, ctx); err == nil {
			t.Fatalf("nil proof at position %d was accepted", nilPos)
		}
	}
}

// TestVerifyRowsWithContextErrorIncludesRow asserts that every error returned
// from VerifyRowsWithContext names the offending row's data index, so the
// fibre wrapper at server_upload.go can identify which row was malformed.
// Covers the shape-validation branches and the scalar fallback path.
func TestVerifyRowsWithContextErrorIncludesRow(t *testing.T) {
	cfg := &Config{K: 64, N: 64, RowSize: 1024, WorkerCount: 1}
	data := make([][]byte, cfg.K)
	r := rand.New(rand.NewPCG(23, 23))
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

	makeBatch := func(n int) []*RowProof {
		proofs := make([]*RowProof, n)
		for i := range proofs {
			p, err := ed.GenerateRowProof(i)
			if err != nil {
				t.Fatalf("GenerateRowProof: %v", err)
			}
			row := append([]byte(nil), p.Row...)
			proofs[i] = &RowProof{Index: p.Index, Row: row, RowProof: p.RowProof}
		}
		return proofs
	}

	tests := []struct {
		name   string
		mutate func(p *RowProof)
		size   int // batch size (≥ minBatchedVerifyK exercises batched, < hits fallback)
	}{
		{"row_size_mismatch", func(p *RowProof) { p.Row = p.Row[:len(p.Row)/2] }, minBatchedVerifyK + 4},
		{"proof_depth_mismatch", func(p *RowProof) { p.RowProof = p.RowProof[:len(p.RowProof)-1] }, minBatchedVerifyK + 4},
		{"scalar_fallback_tampered_row", func(p *RowProof) { p.Row[0] ^= 0xFF }, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _, err := CreateVerificationContext(rlcOrig, cfg)
			if err != nil {
				t.Fatalf("CreateVerificationContext: %v", err)
			}
			proofs := makeBatch(tt.size)
			bad := proofs[2]
			tt.mutate(bad)
			err = VerifyRowsWithContext(proofs, commitment, ctx)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			want := "row " + itoa(bad.Index)
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error missing %q: %q", want, err.Error())
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
