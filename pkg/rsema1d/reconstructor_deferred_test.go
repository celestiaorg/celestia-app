package rsema1d

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/rlc"
)

const (
	deferredK       = 16
	deferredN       = 48
	deferredRowSize = 256
)

func deferredEncode(t *testing.T) (*Coder, *ExtendedData) {
	t.Helper()
	coder, err := NewCoder(&Config{K: deferredK, N: deferredN, WorkerCount: 4})
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(3, 4))
	rows := make([][]byte, deferredK+deferredN)
	for i := range rows {
		rows[i] = make([]byte, deferredRowSize)
		if i < deferredK {
			for j := range rows[i] {
				rows[i][j] = byte(r.IntN(256))
			}
		}
	}
	ed, err := coder.Encode(rows)
	if err != nil {
		t.Fatal(err)
	}
	return coder, ed
}

// deferredProofs returns fresh proofs for indices, with copied rows.
func deferredProofs(t *testing.T, ed *ExtendedData, indices []int) []*RowProof {
	t.Helper()
	proofs := make([]*RowProof, len(indices))
	for i, idx := range indices {
		p, err := ed.GenerateRowProof(idx)
		if err != nil {
			t.Fatal(err)
		}
		p.Row = bytes.Clone(p.Row)
		proofs[i] = p
	}
	return proofs
}

// addInShards adds the proofs four at a time and returns the row buffer.
func addInShards(t *testing.T, r *Reconstructor, proofs []*RowProof, vec rlc.Vector) [][]byte {
	t.Helper()
	rows := make([][]byte, deferredK+deferredN)
	for s := 0; s < len(proofs); s += 4 {
		novel, err := r.Add(append([]*RowProof(nil), proofs[s:min(s+4, len(proofs))]...), vec)
		if err != nil {
			t.Fatalf("Add shard %d: %v", s/4, err)
		}
		for _, p := range novel {
			rows[p.Index] = p.Row
		}
	}
	for i := range deferredK {
		if rows[i] == nil {
			rows[i] = make([]byte, 0, deferredRowSize)
		}
	}
	return rows
}

func indexRange(from, to int) []int {
	out := make([]int, 0, to-from)
	for i := from; i < to; i++ {
		out = append(out, i)
	}
	return out
}

func TestReconstructorDeferredAcceptsValid(t *testing.T) {
	coder, ed := deferredEncode(t)
	for name, indices := range map[string][]int{
		"originals": indexRange(0, deferredK),
		"mixed":     append(indexRange(0, deferredK/2), indexRange(deferredK, deferredK+deferredK/2)...),
		"parity":    indexRange(deferredK, 2*deferredK),
	} {
		r, err := coder.NewReconstructor(ed.Commitment())
		if err != nil {
			t.Fatal(err)
		}
		rows := addInShards(t, r, deferredProofs(t, ed, indices), ed.RLC())
		if err := r.Reconstruct(rows); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for i := range deferredK {
			if !bytes.Equal(rows[i], ed.Row(i)) {
				t.Fatalf("%s: row %d differs", name, i)
			}
		}
	}
}

// TestReconstructorRejectsBadEncoding commits honest rows together with a wrong
// RLC vector: every proof verifies, but the original rows do not match it.
func TestReconstructorRejectsBadEncoding(t *testing.T) {
	coder, ed := deferredEncode(t)
	bad := append(rlc.Vector(nil), ed.RLC()...)
	bad[3] = field.GF128{}
	rowRoot := ed.rowsTree.Root()
	var leaf [field.GF128Size]byte
	rlcRoot := computeRLCRoot(bad, make([]byte, deferredK*merkle.NodeSize), leaf[:])
	commitment := sha256.Sum256(append(rowRoot[:], rlcRoot[:]...))

	r, err := coder.NewReconstructor(commitment)
	if err != nil {
		t.Fatal(err)
	}
	rows := addInShards(t, r, deferredProofs(t, ed, indexRange(0, deferredK)), bad)
	if err := r.Reconstruct(rows); !errors.Is(err, ErrInvalidEncoding) {
		t.Fatalf("expected ErrInvalidEncoding, got %v", err)
	}

	// A parity row is checked against the extension of the bad vector at once.
	if _, err := r.Add(deferredProofs(t, ed, []int{deferredK}), bad); err == nil {
		t.Fatal("parity row accepted against a bad RLC vector")
	}
}

func TestReconstructorRejectsTamperedRowsOnAdd(t *testing.T) {
	coder, ed := deferredEncode(t)
	for _, idx := range []int{0, deferredK - 1, deferredK, deferredK + deferredN - 1} {
		r, err := coder.NewReconstructor(ed.Commitment())
		if err != nil {
			t.Fatal(err)
		}
		proofs := deferredProofs(t, ed, []int{idx})
		proofs[0].Row[7] ^= 1
		if _, err := r.Add(proofs, ed.RLC()); err == nil {
			t.Fatalf("tampered row %d accepted", idx)
		}
		if r.Have() != 0 {
			t.Fatalf("tampered row %d counted", idx)
		}
	}
}
