package rsema1d

import (
	"errors"
	"testing"
)

func TestReconstructorReconstructRequiresEnoughRows(t *testing.T) {
	const (
		k       = 8
		n       = 8
		rowSize = 256
	)

	cfg := &Config{K: k, N: n, RowSize: rowSize, WorkerCount: 1}
	source := make([][]byte, k)
	for i := range source {
		source[i] = make([]byte, rowSize)
		for j := range source[i] {
			source[i][j] = byte(i + j)
		}
	}

	extData, commitment, _, err := Encode(source, cfg)
	if err != nil {
		t.Fatal(err)
	}

	coder, err := NewCoder(&Config{K: k, N: n, WorkerCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := coder.NewReconstructor(commitment)
	if err != nil {
		t.Fatal(err)
	}

	proofs := make([]*RowProof, k)
	for i := range proofs {
		proofs[i], err = extData.GenerateRowProof(i)
		if err != nil {
			t.Fatal(err)
		}
	}

	rows := make([][]byte, k+n)
	for i := range k - 1 {
		rows[i] = extData.rows[i]
	}
	if _, err := rec.Add(proofs[:k-1], extData.RLC()); err != nil {
		t.Fatal(err)
	}

	err = rec.Reconstruct(rows)
	if !errors.Is(err, ErrNotEnoughRows) {
		t.Fatalf("expected ErrNotEnoughRows, got %v", err)
	}
	if want := rec.Want(); want != 1 {
		t.Fatalf("expected Want 1, got %d", want)
	}

	rows[k-1] = extData.rows[k-1]
	if _, err := rec.Add(proofs[k-1:k], extData.RLC()); err != nil {
		t.Fatal(err)
	}
	if err := rec.Reconstruct(rows); err != nil {
		t.Fatal(err)
	}
}
