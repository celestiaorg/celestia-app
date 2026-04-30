package rsema1d

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

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

// TestVerifyRowsWithContextNilProof covers nil elements at several positions,
// ensuring VerifyRowsWithContext returns a clean error rather than panicking
// when a proof slot is nil.
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

	for _, nilPos := range []int{0, 4, 8} {
		ctx, _, err := CreateVerificationContext(rlcOrig, encodeCfg)
		if err != nil {
			t.Fatalf("CreateVerificationContext: %v", err)
		}
		proofs := make([]*RowProof, 12)
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

// TestVerifyRowsWithContextErrorIncludesRow asserts that the row size mismatch
// and proof depth mismatch errors name the offending row's data index, so the
// fibre wrapper at server_upload.go can identify which row was malformed.
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

	tests := []struct {
		name   string
		mutate func(p *RowProof)
	}{
		{"row_size_mismatch", func(p *RowProof) { p.Row = p.Row[:len(p.Row)/2] }},
		{"proof_depth_mismatch", func(p *RowProof) { p.RowProof = p.RowProof[:len(p.RowProof)-1] }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _, err := CreateVerificationContext(rlcOrig, cfg)
			if err != nil {
				t.Fatalf("CreateVerificationContext: %v", err)
			}
			proofs := make([]*RowProof, 12)
			for i := range proofs {
				p, err := ed.GenerateRowProof(i)
				if err != nil {
					t.Fatalf("GenerateRowProof: %v", err)
				}
				row := append([]byte(nil), p.Row...)
				proofs[i] = &RowProof{Index: p.Index, Row: row, RowProof: p.RowProof}
			}
			bad := proofs[2]
			tt.mutate(bad)
			err = VerifyRowsWithContext(proofs, commitment, ctx)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			want := "row " + strconv.Itoa(bad.Index)
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error missing %q: %q", want, err.Error())
			}
		})
	}
}
