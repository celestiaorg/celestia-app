package rsema1d_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
)

func TestReconstructorReconstructRequiresEnoughRows(t *testing.T) {
	const (
		k       = 8
		n       = 8
		rowSize = 256
	)

	cfg := &rsema1d.Config{K: k, N: n, RowSize: rowSize, WorkerCount: 1}
	source := make([][]byte, k)
	for i := range source {
		source[i] = make([]byte, rowSize)
		for j := range source[i] {
			source[i][j] = byte(i + j)
		}
	}

	extData, commitment, _ := encodeRows(t, cfg, source)

	coder, err := rsema1d.NewCoder(&rsema1d.Config{K: k, N: n, WorkerCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := coder.NewReconstructor(commitment)
	if err != nil {
		t.Fatal(err)
	}

	proofs := make([]*rsema1d.RowProof, k)
	for i := range proofs {
		proofs[i], err = extData.GenerateRowProof(i)
		if err != nil {
			t.Fatal(err)
		}
	}

	rows := make([][]byte, k+n)
	for i := range k - 1 {
		rows[i] = extData.Row(i)
	}
	if _, err := rec.Add(proofs[:k-1], extData.RLC()); err != nil {
		t.Fatal(err)
	}

	err = rec.Reconstruct(rows)
	if !errors.Is(err, rsema1d.ErrNotEnoughRows) {
		t.Fatalf("expected ErrNotEnoughRows, got %v", err)
	}
	if want := rec.Want(); want != 1 {
		t.Fatalf("expected Want 1, got %d", want)
	}

	rows[k-1] = extData.Row(k - 1)
	if _, err := rec.Add(proofs[k-1:k], extData.RLC()); err != nil {
		t.Fatal(err)
	}
	if err := rec.Reconstruct(rows); err != nil {
		t.Fatal(err)
	}
}

// TestReconstructorFromVariousSelections verifies the Reconstructor can
// recover the K original rows from any K-sized selection of the K+N extended
// shards: originals-only (trivial roundtrip), parity-only (full RS recovery),
// and a mixed pattern. Runs across the full config matrix so padding /
// boundary issues for non-power-of-2 K show up.
func TestReconstructorFromVariousSelections(t *testing.T) {
	for _, tc := range roundtripConfigs {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &rsema1d.Config{K: tc.k, N: tc.n, RowSize: tc.rowSize, WorkerCount: 1}
			data := fillRows(tc.k, tc.rowSize)
			ed, commitment, rlcOrig := encodeRows(t, cfg, data)

			originals := make([]int, tc.k)
			for i := range originals {
				originals[i] = i
			}
			parities := make([]int, tc.k)
			for i := range parities {
				parities[i] = tc.k + i
			}
			selections := []struct {
				name    string
				indices []int
			}{
				{"original_rows", originals},
				{"parity_rows", parities},
				{"mixed_rows", reconstructMixedIndices(tc.k, tc.n)},
			}

			for _, sel := range selections {
				t.Run(sel.name, func(t *testing.T) {
					coder, err := rsema1d.NewCoder(cfg)
					if err != nil {
						t.Fatalf("NewCoder: %v", err)
					}
					rec, err := coder.NewReconstructor(commitment)
					if err != nil {
						t.Fatalf("NewReconstructor: %v", err)
					}

					proofs := proofsAtIndices(t, ed, sel.indices)
					if _, err := rec.Add(proofs, rlcOrig); err != nil {
						t.Fatalf("Add: %v", err)
					}

					rows := make([][]byte, cfg.K+cfg.N)
					for _, idx := range sel.indices {
						rows[idx] = ed.Row(idx)
					}
					if err := rec.Reconstruct(rows); err != nil {
						t.Fatalf("Reconstruct: %v", err)
					}

					for i := range cfg.K {
						if !bytes.Equal(rows[i], data[i]) {
							t.Fatalf("original row %d not recovered", i)
						}
					}
				})
			}
		})
	}
}

// reconstructMixedIndices picks K unique indices interleaved across the K+N
// extended range to exercise mixed original+parity recovery.
func reconstructMixedIndices(k, n int) []int {
	step := (k + n) / k
	indices := make([]int, k)
	seen := make(map[int]bool)
	for i := range k {
		idx := (i * step) % (k + n)
		for seen[idx] {
			idx = (idx + 1) % (k + n)
		}
		indices[i] = idx
		seen[idx] = true
	}
	return indices
}
