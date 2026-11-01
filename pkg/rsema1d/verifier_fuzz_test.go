package rsema1d

import (
	"bytes"
	"errors"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/rlc"
)

// Small shape so each fuzz iteration encodes and verifies quickly. K+N = 8
// gives a proof depth of 3.
const (
	fuzzK = 4
	fuzzN = 4
)

// fuzzOps consumes a fuzzer-chosen byte stream; an exhausted stream yields 0.
type fuzzOps []byte

func (o *fuzzOps) next() byte {
	if len(*o) == 0 {
		return 0
	}
	b := (*o)[0]
	*o = (*o)[1:]
	return b
}

// fuzzMatrix is the committed matrix a fuzz iteration verifies against.
// committed may differ from honest where the fuzzer poisoned rows; the RLC
// values are always computed from the honest originals.
type fuzzMatrix struct {
	coder      *Coder
	rowSize    int
	honest     [][]byte // honest K+N extended rows
	committed  [][]byte // rows the row tree commits to
	poisoned   []bool
	tree       *merkle.Tree
	rlc        rlc.Vector
	commitment Commitment
}

// newFuzzMatrix encodes K rows filled cyclically from data. Rows whose bit is
// set in poison are replaced in the committed tree by garbage of the same size
// or, with wideGarbage, of twice the size.
func newFuzzMatrix(t *testing.T, data []byte, sizeSel, poison byte, wideGarbage bool) *fuzzMatrix {
	t.Helper()
	cfg := &Config{K: fuzzK, N: fuzzN, WorkerCount: 1}
	coder, err := NewCoder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rowSize := (int(sizeSel%4) + 1) * field.LeopardChunkSize
	rows := make([][]byte, fuzzK+fuzzN)
	for i := range rows {
		rows[i] = make([]byte, rowSize)
		if i < fuzzK && len(data) > 0 {
			for j := range rows[i] {
				rows[i][j] = data[(i*rowSize+j)%len(data)]
			}
		}
	}
	ed, err := coder.Encode(rows)
	if err != nil {
		t.Fatal(err)
	}

	m := &fuzzMatrix{
		coder:     coder,
		rowSize:   rowSize,
		honest:    make([][]byte, fuzzK+fuzzN),
		committed: make([][]byte, fuzzK+fuzzN),
		poisoned:  make([]bool, fuzzK+fuzzN),
		rlc:       append(rlc.Vector(nil), ed.RLC()...),
	}
	for i := range m.honest {
		m.honest[i] = bytes.Clone(ed.Row(i))
		m.committed[i] = m.honest[i]
		if poison&(1<<i) != 0 {
			size := rowSize
			if wideGarbage {
				size *= 2
			}
			m.committed[i] = bytes.Repeat([]byte{0xA5 ^ byte(i)}, size)
			m.poisoned[i] = true
		}
	}
	m.tree = merkle.NewTree(nil, m.committed, 1)
	if poison != 0 {
		// The coefficients depend on the poisoned row root.
		coeffs := rlc.DeriveCoefficients(m.tree.Root(), fuzzK, fuzzN, rowSize, 1)
		m.rlc = rlc.Compute(m.honest[:fuzzK], coeffs, 1)
	}
	m.commitment = commitmentFor(cfg, m.tree.Root(), m.rlc)
	if poison == 0 && m.commitment != ed.Commitment() {
		t.Fatal("fuzz commitment helper disagrees with Coder.Encode")
	}
	return m
}

// proofs returns fresh proofs for indices, cloning every row and path node
// so mutations never touch the matrix.
func (m *fuzzMatrix) proofs(t *testing.T, indices []int) []*RowProof {
	t.Helper()
	out := make([]*RowProof, len(indices))
	for i, idx := range indices {
		path, err := m.tree.Proof(idx)
		if err != nil {
			t.Fatal(err)
		}
		cloned := make([][]byte, len(path))
		for j := range path {
			cloned[j] = bytes.Clone(path[j])
		}
		out[i] = &RowProof{Index: idx, Row: bytes.Clone(m.committed[idx]), RowProof: cloned}
	}
	return out
}

// mutateProofs applies up to maxOps fuzzer-chosen mutations to proofs, the RLC
// vector and the commitment.
func mutateProofs(ops *fuzzOps, maxOps int, proofs []*RowProof, vec rlc.Vector, commitment *Commitment) []*RowProof {
	for n := 0; n < maxOps && len(*ops) > 0; n++ {
		op := ops.next() % 13
		if len(proofs) == 0 && op != 12 {
			continue
		}
		var p *RowProof
		if len(proofs) > 0 {
			p = proofs[int(ops.next())%len(proofs)]
		}
		if p == nil && op < 6 {
			continue
		}
		switch op {
		case 0: // flip a row byte
			if len(p.Row) > 0 {
				p.Row[int(ops.next())*7%len(p.Row)] ^= ops.next() | 1
			}
		case 1: // flip a path byte
			if len(p.RowProof) > 0 {
				node := p.RowProof[int(ops.next())%len(p.RowProof)]
				if len(node) > 0 {
					node[int(ops.next())%len(node)] ^= ops.next() | 1
				}
			}
		case 2: // arbitrary, possibly negative or out-of-range, index
			p.Index = int(int8(ops.next()))
		case 3: // resize the row to an arbitrary length
			size := int(ops.next()) * 3
			row := make([]byte, size)
			copy(row, p.Row)
			p.Row = row
		case 4: // shrink, grow or resize a path
			switch ops.next() % 3 {
			case 0:
				if len(p.RowProof) > 0 {
					p.RowProof = p.RowProof[:len(p.RowProof)-1]
				}
			case 1:
				p.RowProof = append(p.RowProof, make([]byte, merkle.NodeSize))
			default:
				if len(p.RowProof) > 0 {
					i := int(ops.next()) % len(p.RowProof)
					p.RowProof[i] = make([]byte, int(ops.next())%(2*merkle.NodeSize))
				}
			}
		case 5: // row nil vs empty
			p.Row = nil
		case 6: // nil entry
			proofs[int(ops.next())%len(proofs)] = nil
		case 7: // duplicate an entry
			proofs = append(proofs, proofs[int(ops.next())%len(proofs)])
		case 8: // drop an entry
			i := int(ops.next()) % len(proofs)
			proofs = append(proofs[:i], proofs[i+1:]...)
		case 9: // swap the rows of two entries
			q := proofs[int(ops.next())%len(proofs)]
			if p != nil && q != nil {
				p.Row, q.Row = q.Row, p.Row
			}
		case 10: // flip an RLC byte
			if len(vec) > 0 {
				vec[int(ops.next())%len(vec)][ops.next()%field.GF128Width] ^= uint16(ops.next()) | 1
			}
		case 11: // flip a commitment byte
			commitment[ops.next()%32] ^= ops.next() | 1
		case 12: // append an entry with an arbitrary index and empty data
			proofs = append(proofs, &RowProof{Index: int(int8(ops.next()))})
		}
	}
	return proofs
}

// excludedNilFirstProof reports inputs that put a nil proof first, which are
// excluded below.
func excludedNilFirstProof(proofs []*RowProof) bool {
	return len(proofs) > 0 && proofs[0] == nil
}

// FuzzVerifier checks Verifier.Verify and VerifyShared, which validate row
// proofs from fibre upload peers, against mutated proofs, rows, RLC values and
// commitments. Neither may panic, and an accepted batch must consist of the
// committed rows under the honest commitment and RLC values. A pooled verifier
// must stay usable after any input.
func FuzzVerifier(f *testing.F) {
	f.Add([]byte("rows"), byte(0), byte(0x0f), []byte{})
	f.Add([]byte("parity"), byte(1), byte(0xf0), []byte{})
	f.Add([]byte{1, 2, 3}, byte(3), byte(0xff), []byte{})
	f.Add([]byte("tamper"), byte(0), byte(0x13), []byte{0, 0, 5, 1})
	f.Add([]byte("index"), byte(2), byte(0x81), []byte{2, 1, 0x80, 3, 0, 9})
	f.Add([]byte("rlc"), byte(0), byte(0x0f), []byte{10, 0, 1, 2, 3, 11, 4, 5})
	f.Add([]byte("shape"), byte(1), byte(0x33), []byte{4, 0, 0, 7, 1, 8, 0, 12, 40, 6, 2, 1})

	f.Fuzz(func(t *testing.T, data []byte, sizeSel, sel byte, ops []byte) {
		m := newFuzzMatrix(t, data, sizeSel, 0, false)
		var indices []int
		for i := range fuzzK + fuzzN {
			if sel&(1<<i) != 0 {
				indices = append(indices, i)
			}
		}
		vec := append(rlc.Vector(nil), m.rlc...)
		commitment := m.commitment
		o := fuzzOps(ops)
		proofs := mutateProofs(&o, 16, m.proofs(t, indices), vec, &commitment)
		// Excluded pending a fix: https://linear.app/celestia/issue/PROTOCO-3525.
		if excludedNilFirstProof(proofs) {
			return
		}

		v, err := NewVerifier(m.coder.config)
		if err != nil {
			t.Fatal(err)
		}
		checkAccepted := func(name string, err error) {
			if err != nil {
				return
			}
			if commitment != m.commitment {
				t.Fatalf("%s accepted a mutated commitment", name)
			}
			for i := range vec {
				if !field.Equal128(vec[i], m.rlc[i]) {
					t.Fatalf("%s accepted mutated RLC value %d", name, i)
				}
			}
			for _, p := range proofs {
				if !bytes.Equal(p.Row, m.honest[p.Index]) {
					t.Fatalf("%s accepted a row at index %d that differs from the encoding", name, p.Index)
				}
			}
		}
		checkAccepted("Verify", v.Verify(commitment, proofs, vec))

		// VerifyShared reuses the RLC values the last Verify cached. Prime a
		// second verifier with an honest batch, then hand it the mutated batch
		// whatever Verify decided, so malformed input reaches the shared path.
		shared, err := NewVerifier(m.coder.config)
		if err != nil {
			t.Fatal(err)
		}
		if err := shared.Verify(m.commitment, m.proofs(t, []int{0, fuzzK}), m.rlc); err != nil {
			t.Fatalf("honest priming batch rejected: %v", err)
		}
		if shared.VerifyShared(commitment, proofs) == nil {
			if commitment != m.commitment {
				t.Fatal("VerifyShared accepted a mutated commitment")
			}
			for _, p := range proofs {
				if !bytes.Equal(p.Row, m.honest[p.Index]) {
					t.Fatalf("VerifyShared accepted a row at index %d that differs from the encoding", p.Index)
				}
			}
		}

		// The verifier is pooled across uploads: an honest batch must still pass.
		if err := v.Verify(m.commitment, m.proofs(t, []int{0, fuzzK}), m.rlc); err != nil {
			t.Fatalf("honest batch rejected after fuzz input: %v", err)
		}
	})
}

// FuzzReconstructor drives the download-path Reconstructor with interleaved
// Add, Reject and Reconstruct calls over mutated proofs, optionally against a
// commitment whose row tree poisons some rows. Nothing may panic; Add may only
// return committed rows; Reconstruct may only flag poisoned rows; and any
// successful reconstruction must re-encode to the given commitment (or, for a
// poisoned commitment, equal the honest originals the RLC values commit to).
func FuzzReconstructor(f *testing.F) {
	// add encodes an unmutated Add of indices; reconstruct encodes Reconstruct.
	add := func(indices ...byte) []byte {
		return append(append([]byte{0, byte(len(indices) - 1)}, indices...), 0)
	}
	reconstruct := []byte{3}
	seq := func(steps ...[]byte) []byte { return bytes.Join(steps, nil) }
	all := seq(add(0, 1, 2, 3), add(4, 5, 6, 7), reconstruct)
	f.Add([]byte("rows"), byte(0), byte(0), false, all)
	f.Add([]byte("parity"), byte(1), byte(0), false, seq(add(4, 5, 6, 7), reconstruct))
	f.Add([]byte("mixed"), byte(2), byte(0), false, seq(add(0, 6), add(2, 7, 0), reconstruct, add(5), reconstruct))
	f.Add([]byte("poison"), byte(0), byte(0x02), false, seq(add(0, 2, 3), add(1), reconstruct, add(4), reconstruct))
	f.Add([]byte("wide"), byte(1), byte(0x01), true, seq(add(1, 2, 3), add(0), add(4), reconstruct))
	f.Add([]byte("reject"), byte(0), byte(0), false, seq(all, []byte{2, 0, 1}, reconstruct, add(1), reconstruct))
	f.Add([]byte("mutate"), byte(3), byte(0), false, seq([]byte{0, 3, 0, 1, 2, 3, 2, 0, 1, 0, 7, 1, 4}, add(1, 4), reconstruct))

	f.Fuzz(func(t *testing.T, data []byte, sizeSel, poison byte, wideGarbage bool, ops []byte) {
		m := newFuzzMatrix(t, data, sizeSel, poison, wideGarbage)
		r, err := m.coder.NewReconstructor(m.commitment)
		if err != nil {
			t.Fatal(err)
		}
		o := fuzzOps(ops)
		rows := make([][]byte, fuzzK+fuzzN)
		clearSlots := func(indices []int) {
			for _, idx := range indices {
				if idx >= 0 && idx < fuzzK {
					rows[idx] = nil
				}
			}
		}

		for step := 0; step < 24 && len(o) > 0; step++ {
			switch o.next() % 4 {
			case 0, 1: // Add a batch, possibly mutated
				n := int(o.next())%(fuzzK+fuzzN) + 1
				indices := make([]int, n)
				for i := range indices {
					indices[i] = int(o.next()) % (fuzzK + fuzzN)
				}
				vec := append(rlc.Vector(nil), m.rlc...)
				commitment := m.commitment
				proofs := mutateProofs(&o, int(o.next())%4, m.proofs(t, indices), vec, &commitment)
				// Excluded pending a fix: https://linear.app/celestia/issue/PROTOCO-3525.
				if excludedNilFirstProof(proofs) {
					continue
				}
				novel, err := r.Add(proofs, vec)
				if err != nil {
					continue
				}
				for _, p := range novel {
					if !bytes.Equal(p.Row, m.committed[p.Index]) {
						t.Fatalf("Add accepted a row at index %d that is not committed", p.Index)
					}
					if rows[p.Index] != nil {
						t.Fatalf("Add returned index %d twice", p.Index)
					}
					rows[p.Index] = p.Row
				}
			case 2: // Reject arbitrary indices
				indices := make([]int, int(o.next())%3+1)
				for i := range indices {
					indices[i] = int(int8(o.next()))
				}
				r.Reject(indices)
				clearSlots(indices)
			case 3: // Reconstruct on a copy of the row buffer
				buf := append([][]byte(nil), rows...)
				err := r.Reconstruct(buf)
				var invalid *InvalidRowsError
				switch {
				case errors.As(err, &invalid):
					for _, idx := range invalid.Indices {
						if idx < 0 || idx >= fuzzK || !m.poisoned[idx] {
							t.Fatalf("Reconstruct flagged honest row %d", idx)
						}
					}
					r.Reject(invalid.Indices)
					clearSlots(invalid.Indices)
				case err == nil:
					checkReconstructed(t, m, buf)
				}
			}
		}
	})
}

// checkReconstructed requires reconstructed originals to re-encode to the
// commitment. With a poisoned commitment no encoding matches it, so the
// originals must instead be the honest rows its RLC values commit to.
func checkReconstructed(t *testing.T, m *fuzzMatrix, buf [][]byte) {
	t.Helper()
	poisoned := false
	for _, p := range m.poisoned {
		poisoned = poisoned || p
	}
	if poisoned {
		for i := range fuzzK {
			if !bytes.Equal(buf[i], m.honest[i]) {
				t.Fatalf("reconstructed original %d differs from the RLC-committed row", i)
			}
		}
		return
	}
	extended := make([][]byte, fuzzK+fuzzN)
	for i := range extended {
		if i < fuzzK {
			if len(buf[i]) != m.rowSize {
				t.Fatalf("reconstructed original %d has %d bytes, want %d", i, len(buf[i]), m.rowSize)
			}
			extended[i] = bytes.Clone(buf[i])
		} else {
			extended[i] = make([]byte, m.rowSize)
		}
	}
	ed, err := m.coder.Encode(extended)
	if err != nil {
		t.Fatal(err)
	}
	if ed.Commitment() != m.commitment {
		t.Fatal("reconstructed rows do not re-encode to the commitment")
	}
}
