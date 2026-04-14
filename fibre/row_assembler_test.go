package fibre

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
)

func TestRowAssembler_Assemble(t *testing.T) {
	codecCfg := &rsema1d.Config{K: 4, N: 2, WorkerCount: 1}
	allocCfg := DefaultRowAssemblerConfig()
	allocCfg.MaxRowSize = 256
	allocCfg.PackedChunkSize = 4096

	alloc, err := NewRowAssembler(codecCfg, allocCfg)
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

	rows, release := alloc.Assemble(data, rowSize, offset)
	defer release()

	if len(rows) != codecCfg.K+codecCfg.N {
		t.Fatalf("got %d rows, want %d", len(rows), codecCfg.K+codecCfg.N)
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
	for i := 3; i < codecCfg.K; i++ {
		if &rows[3][0] != &rows[i][0] {
			t.Fatal("empty original rows do not share the zero row")
		}
	}

	// parity rows are zeroed
	for i := codecCfg.K; i < codecCfg.K+codecCfg.N; i++ {
		for j, b := range rows[i] {
			if b != 0 {
				t.Fatalf("parity row %d byte %d = %d, want 0", i, j, b)
			}
		}
	}
}

func TestRowAssembler_Assemble_AllRowSizes(t *testing.T) {
	k, n := 64, 64
	maxRowSize := 4096

	codecCfg := &rsema1d.Config{K: k, N: n, WorkerCount: 1}
	allocCfg := DefaultRowAssemblerConfig()
	allocCfg.MaxRowSize = maxRowSize
	alloc, err := NewRowAssembler(codecCfg, allocCfg)
	if err != nil {
		t.Fatalf("NewRowAssembler: %v", err)
	}

	for rowSize := 64; rowSize <= maxRowSize; rowSize += 64 {
		data := make([]byte, max(1, k*rowSize-5))
		rows, release := alloc.Assemble(data, rowSize, 5)

		if len(rows) != k+n {
			t.Fatalf("rowSize %d: got %d rows, want %d", rowSize, len(rows), k+n)
		}
		for i, row := range rows {
			if len(row) != rowSize {
				t.Fatalf("rowSize %d: row %d len %d", rowSize, i, len(row))
			}
		}

		release()
	}
}

func TestRowAssembler_InvalidConfig(t *testing.T) {
	codecCfg := &rsema1d.Config{K: 32, N: 32, WorkerCount: 1}
	_, err := NewRowAssembler(codecCfg, &RowAssemblerConfig{
		MaxRowSize:      256,
		PackedChunkSize: 0,
		MaxRowPoolCap:   1,
	})
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestRowAssembler_ReuseDirty(t *testing.T) {
	k, n := 4, 4
	tests := []struct {
		name       string
		rowSize    int
		maxRowSize int
	}{
		{"packed", 64, 256},
		{"max", 256, 256},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codecCfg := &rsema1d.Config{K: k, N: n, WorkerCount: 1}
			coder, _ := rsema1d.NewCoder(codecCfg)
			allocCfg := DefaultRowAssemblerConfig()
			allocCfg.MaxRowSize = tc.maxRowSize
			alloc, _ := NewRowAssembler(codecCfg, allocCfg)

			// first encode
			data1 := make([]byte, k*tc.rowSize-7)
			for i := range data1 {
				data1[i] = byte(i + 1)
			}
			rows1, release1 := alloc.Assemble(data1, tc.rowSize, 7)
			if _, err := coder.Encode(rows1); err != nil {
				t.Fatalf("first Encode: %v", err)
			}
			release1()

			// second encode — parity must be clean
			data2 := make([]byte, k*tc.rowSize-7)
			for i := range data2 {
				data2[i] = byte(i*3 + 7)
			}
			rows2, release2 := alloc.Assemble(data2, tc.rowSize, 7)
			defer release2()

			for i := k; i < k+n; i++ {
				for j, b := range rows2[i] {
					if b != 0 {
						t.Fatalf("parity row %d byte %d = %d, want 0", i, j, b)
					}
				}
			}

			// pooled encode must match fresh encode
			extPool, err := coder.Encode(rows2)
			if err != nil {
				t.Fatalf("pooled Encode: %v", err)
			}

			fresh := make([][]byte, k+n)
			for i := range fresh {
				fresh[i] = make([]byte, tc.rowSize)
			}
			n0 := min(tc.rowSize-7, len(data2))
			copy(fresh[0][7:], data2[:n0])
			off := n0
			for i := 1; i < k && off < len(data2); i++ {
				end := min(off+tc.rowSize, len(data2))
				copy(fresh[i], data2[off:end])
				off += tc.rowSize
			}
			extFresh, err := coder.Encode(fresh)
			if err != nil {
				t.Fatalf("fresh Encode: %v", err)
			}

			if extPool.Commitment() != extFresh.Commitment() {
				t.Fatal("pooled and fresh commitments differ")
			}
		})
	}
}
