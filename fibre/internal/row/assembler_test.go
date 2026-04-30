package row

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
)

func TestAssembler_Assemble(t *testing.T) {
	const k, n = 4, 2
	a, err := NewAssembler(k, n, 256)
	if err != nil {
		t.Fatalf("NewAssembler: %v", err)
	}

	rowSize := 64
	offset := 5
	// data spans: partial first row + 1 full row + 3 bytes into a third row
	data := make([]byte, rowSize-offset+rowSize+3)
	for i := range data {
		data[i] = byte(i % 251)
	}

	asm := a.Assemble(data, rowSize, offset)
	defer asm.Free()
	rows := asm.Rows()

	if len(rows) != k+n {
		t.Fatalf("got %d rows, want %d", len(rows), k+n)
	}
	for i, r := range rows {
		if len(r) != rowSize {
			t.Fatalf("row %d: len %d, want %d", i, len(r), rowSize)
		}
	}

	// full middle row aliases input data
	fullRowStart := rowSize - offset
	rows[1][0] ^= 0xFF
	if rows[1][0] != data[fullRowStart] {
		t.Fatal("full middle row does not alias input data")
	}

	// the partial trailing row is a copy, not an alias
	partialStart := fullRowStart + rowSize
	rows[2][0] ^= 0xFF
	if rows[2][0] == data[partialStart] {
		t.Fatal("partial trailing row unexpectedly aliases input data")
	}

	// empty original rows share a single backing array
	for i := 3; i < k; i++ {
		if &rows[3][0] != &rows[i][0] {
			t.Fatal("empty original rows do not share the zero row")
		}
	}

	// parity rows are zeroed
	for i := k; i < k+n; i++ {
		for j, b := range rows[i] {
			if b != 0 {
				t.Fatalf("parity row %d byte %d = %d, want 0", i, j, b)
			}
		}
	}
}

func TestAssembler_InvalidConfig(t *testing.T) {
	if _, err := NewAssembler(0, 4, 64); err == nil {
		t.Fatal("expected error for non-positive k")
	}
	if _, err := NewAssembler(4, 0, 64); err == nil {
		t.Fatal("expected error for non-positive n")
	}
}

// TestAssembler_ReuseDirty verifies that reusing the assembler does not
// leak state between encodes: parity rows are zeroed on reallocation and the
// pooled encoding matches a fresh from-scratch encoding of the same data.
func TestAssembler_ReuseDirty(t *testing.T) {
	const k, n, rowSize, offset = 4, 4, 64, 7
	codec := &rsema1d.Config{K: k, N: n, WorkerCount: 1}
	coder, _ := rsema1d.NewCoder(codec)
	a, _ := NewAssembler(k, n, 256)

	// first encode dirties parity slots in the pool
	data1 := make([]byte, k*rowSize-offset)
	for i := range data1 {
		data1[i] = byte(i + 1)
	}
	asm1 := a.Assemble(data1, rowSize, offset)
	if _, err := coder.Encode(asm1.Rows()); err != nil {
		t.Fatalf("first Encode: %v", err)
	}
	asm1.Free()

	// second encode — parity must be clean on reuse
	data2 := make([]byte, k*rowSize-offset)
	for i := range data2 {
		data2[i] = byte(i*3 + 7)
	}
	asm2 := a.Assemble(data2, rowSize, offset)
	defer asm2.Free()
	rows2 := asm2.Rows()

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

// A small blob reusing a pooled slab from a larger one must not carry
// the larger blob's bytes in the prefixed/partial buffers.
func TestAssembler_ReusePartialPadding(t *testing.T) {
	const k, n, rowSize, offset = 4, 4, 64, 7
	codec := &rsema1d.Config{K: k, N: n, WorkerCount: 1}
	coder, _ := rsema1d.NewCoder(codec)
	a, _ := NewAssembler(k, n, 256)

	big := make([]byte, k*rowSize-offset)
	for i := range big {
		big[i] = 0xFF
	}
	asm1 := a.Assemble(big, rowSize, offset)
	if _, err := coder.Encode(asm1.Rows()); err != nil {
		t.Fatalf("first Encode: %v", err)
	}
	asm1.Free()

	const small = 10
	tiny := make([]byte, small)
	for i := range tiny {
		tiny[i] = byte(i + 1)
	}
	asm2 := a.Assemble(tiny, rowSize, offset)
	defer asm2.Free()
	rows2 := asm2.Rows()

	for i := offset + small; i < rowSize; i++ {
		if rows2[0][i] != 0 {
			t.Fatalf("prefixed[%d] = 0x%x after pool reuse, want 0", i, rows2[0][i])
		}
	}

	extPool, err := coder.Encode(rows2)
	if err != nil {
		t.Fatalf("pooled Encode: %v", err)
	}
	extFresh, err := coder.Encode(freshRows(tiny, k+n, rowSize, offset))
	if err != nil {
		t.Fatalf("fresh Encode: %v", err)
	}
	if extPool.Commitment() != extFresh.Commitment() {
		t.Fatal("pooled and fresh commitments differ — stale padding leaked")
	}
}

// freshRows rebuilds the originalRows+parityRows layout that Assemble
// produces, using fresh allocations instead of pooled storage. Used by
// ReuseDirty to cross-check.
func freshRows(data []byte, total, rowSize, offset int) [][]byte {
	rows := make([][]byte, total)
	for i := range rows {
		rows[i] = make([]byte, rowSize)
	}
	n := min(rowSize-offset, len(data))
	copy(rows[0][offset:], data[:n])
	for i, off := 1, n; i < total && off < len(data); i++ {
		n := copy(rows[i], data[off:])
		off += n
	}
	return rows
}

// TestAssembly_Free covers whole-slab release: Rows() and Released()
// observe the transition atomically and repeated calls are no-ops.
func TestAssembly_Free(t *testing.T) {
	const k, n = 4, 4
	a, _ := NewAssembler(k, n, 256)

	data := make([]byte, 4*64-3)
	asm := a.Assemble(data, 64, 3)

	if asm.Released() {
		t.Fatal("fresh Assembly should not report Released")
	}
	if asm.Rows() == nil {
		t.Fatal("fresh Assembly should expose Rows")
	}

	asm.Free()
	asm.Free() // idempotent

	if !asm.Released() {
		t.Fatal("Released() should be true after Free")
	}
	if asm.Rows() != nil {
		t.Fatal("Rows() should be nil after Free")
	}
}
