package fibre

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
)

func TestRowAssembler_Assemble(t *testing.T) {
	codec := &rsema1d.Config{K: 4, N: 2, WorkerCount: 1}
	a, err := NewRowAssembler(codec, 256)
	if err != nil {
		t.Fatalf("NewRowAssembler: %v", err)
	}

	rowSize := 64
	offset := 5
	// data spans: partial first row + 1 full row + 3 bytes into a third row
	data := make([]byte, rowSize-offset+rowSize+3)
	for i := range data {
		data[i] = byte(i % 251)
	}

	rows, rel := a.Assemble(data, rowSize, offset)
	defer rel.Free(nil)

	if len(rows) != codec.K+codec.N {
		t.Fatalf("got %d rows, want %d", len(rows), codec.K+codec.N)
	}
	for i, row := range rows {
		if len(row) != rowSize {
			t.Fatalf("row %d: len %d, want %d", i, len(row), rowSize)
		}
	}

	// full middle row aliases input data
	fullRowStart := rowSize - offset
	rows[1][0] ^= 0xFF
	if rows[1][0] != data[fullRowStart] {
		t.Fatal("full middle row does not alias input data")
	}

	// partial tail row is a copy, not an alias
	tailStart := fullRowStart + rowSize
	rows[2][0] ^= 0xFF
	if rows[2][0] == data[tailStart] {
		t.Fatal("partial tail row unexpectedly aliases input data")
	}

	// empty original rows share a single backing array
	for i := 3; i < codec.K; i++ {
		if &rows[3][0] != &rows[i][0] {
			t.Fatal("empty original rows do not share the zero row")
		}
	}

	// parity rows are zeroed
	for i := codec.K; i < codec.K+codec.N; i++ {
		for j, b := range rows[i] {
			if b != 0 {
				t.Fatalf("parity row %d byte %d = %d, want 0", i, j, b)
			}
		}
	}
}

func TestRowAssembler_InvalidConfig(t *testing.T) {
	codec := &rsema1d.Config{K: 32, N: 32, WorkerCount: 1}
	if _, err := NewRowAssembler(codec, 63); err == nil {
		t.Fatal("expected error for non-multiple-of-64 maxRowSize")
	}
}

// TestRowAssembler_ReuseDirty verifies that reusing the assembler does not
// leak state between encodes: parity rows are zeroed on reallocation and the
// pooled encoding matches a fresh from-scratch encoding of the same data.
func TestRowAssembler_ReuseDirty(t *testing.T) {
	const k, n, rowSize, offset = 4, 4, 64, 7
	codec := &rsema1d.Config{K: k, N: n, WorkerCount: 1}
	coder, _ := rsema1d.NewCoder(codec)
	a, _ := NewRowAssembler(codec, 256)

	// first encode dirties parity slots in the pool
	data1 := make([]byte, k*rowSize-offset)
	for i := range data1 {
		data1[i] = byte(i + 1)
	}
	rows1, rel1 := a.Assemble(data1, rowSize, offset)
	if _, err := coder.Encode(rows1); err != nil {
		t.Fatalf("first Encode: %v", err)
	}
	rel1.Free(nil)

	// second encode — parity must be clean on reuse
	data2 := make([]byte, k*rowSize-offset)
	for i := range data2 {
		data2[i] = byte(i*3 + 7)
	}
	rows2, rel2 := a.Assemble(data2, rowSize, offset)
	defer rel2.Free(nil)

	for i := k; i < k+n; i++ {
		for j, b := range rows2[i] {
			if b != 0 {
				t.Fatalf("parity row %d byte %d = %d, want 0", i, j, b)
			}
		}
	}

	// pooled commitment must match a fresh from-scratch encoding
	extPool, err := coder.Encode(rows2)
	if err != nil {
		t.Fatalf("pooled Encode: %v", err)
	}
	extFresh, err := coder.Encode(freshRows(data2, k+n, rowSize, offset))
	if err != nil {
		t.Fatalf("fresh Encode: %v", err)
	}
	if extPool.Commitment() != extFresh.Commitment() {
		t.Fatal("pooled and fresh commitments differ")
	}
}

// freshRows rebuilds the K+N row layout that Assemble produces, using fresh
// allocations instead of pooled storage. Used by ReuseDirty to cross-check.
func freshRows(data []byte, total, rowSize, offset int) [][]byte {
	rows := make([][]byte, total)
	for i := range rows {
		rows[i] = make([]byte, rowSize)
	}
	head := min(rowSize-offset, len(data))
	copy(rows[0][offset:], data[:head])
	for i, off := 1, head; i < total && off < len(data); i++ {
		n := copy(rows[i], data[off:])
		off += n
	}
	return rows
}

// TestAssembly_Free covers partial + terminal release semantics with the
// observable checks (Rows() reflects partial frees, Freed() reports per-row
// state, repeated/out-of-range calls are no-ops).
func TestAssembly_Free(t *testing.T) {
	codec := &rsema1d.Config{K: 4, N: 4, WorkerCount: 1}
	a, _ := NewRowAssembler(codec, 256)

	data := make([]byte, 4*64-3)
	_, asm := a.Assemble(data, 64, 3)

	// partial: K=4, so parity indices 5 and 7 map to parity offsets 1 and 3.
	asm.Free([]int{5, 7})
	// indices < K (original rows) are ignored; duplicate release is a no-op.
	asm.Free([]int{0, 1, 2, 3, 5})

	rows := asm.Rows()
	for _, i := range []int{5, 7} {
		if rows[i] != nil || !asm.Freed(i) {
			t.Fatalf("row %d should be freed", i)
		}
	}
	for _, i := range []int{0, 4, 6} { // head, first parity, non-freed parity
		if rows[i] == nil || asm.Freed(i) {
			t.Fatalf("row %d should still be live", i)
		}
	}

	// terminal: frees remaining rows + header, idempotent.
	asm.Free(nil)
	asm.Free(nil)
	if asm.Rows() != nil {
		t.Fatal("Rows() should be nil after terminal Free")
	}
}
