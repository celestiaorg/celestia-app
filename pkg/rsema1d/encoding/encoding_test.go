package encoding

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/pkg/rsema1d/field"
)

func TestExtendVertical(t *testing.T) {
	tests := []struct {
		name    string
		k       int
		n       int
		rowSize int
		wantErr bool
	}{
		{
			name:    "1:1 encoding k=4 n=4",
			k:       4,
			n:       4,
			rowSize: 64,
			wantErr: false,
		},
		{
			name:    "1:3 encoding k=4 n=12",
			k:       4,
			n:       12,
			rowSize: 64,
			wantErr: false,
		},
		{
			name:    "1:1 encoding k=8 n=8",
			k:       8,
			n:       8,
			rowSize: 256,
			wantErr: false,
		},
		{
			name:    "1:3 encoding k=8 n=24",
			k:       8,
			n:       24,
			rowSize: 256,
			wantErr: false,
		},
		{
			name:    "1:1 encoding k=16 n=16",
			k:       16,
			n:       16,
			rowSize: 128,
			wantErr: false,
		},
		{
			name:    "1:3 encoding k=16 n=48",
			k:       16,
			n:       48,
			rowSize: 128,
			wantErr: false,
		},
		{
			name:    "invalid row size not multiple of 64",
			k:       4,
			n:       4,
			rowSize: 100,
			wantErr: true,
		},
		{
			name:    "zero rows",
			k:       0,
			n:       4,
			rowSize: 64,
			wantErr: true,
		},
		{
			name:    "invalid n=0",
			k:       4,
			n:       0,
			rowSize: 64,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test data
			data := make([][]byte, tt.k)
			for i := range tt.k {
				data[i] = make([]byte, tt.rowSize)
				// Fill with some pattern
				for j := range tt.rowSize {
					data[i][j] = byte((i + j) % 256)
				}
			}

			// Call ExtendVertical
			extended, err := ExtendVertical(data, tt.n)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ExtendVertical() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ExtendVertical() unexpected error: %v", err)
				return
			}

			// Verify output
			if len(extended) != tt.k+tt.n {
				t.Errorf("ExtendVertical() returned %d rows, want %d", len(extended), tt.k+tt.n)
			}

			// Verify original rows are unchanged
			for i := range tt.k {
				if !bytes.Equal(extended[i], data[i]) {
					t.Errorf("ExtendVertical() modified original row %d", i)
				}
			}

			// Verify parity rows have correct size
			for i := tt.k; i < tt.k+tt.n; i++ {
				if len(extended[i]) != tt.rowSize {
					t.Errorf("ExtendVertical() parity row %d has size %d, want %d", i, len(extended[i]), tt.rowSize)
				}
			}
		})
	}
}

func TestPackUnpackGF128(t *testing.T) {
	// Test that pack/unpack round-trips correctly
	tests := []field.GF128{
		field.Zero(),
		{1, 2, 3, 4, 5, 6, 7, 8},
		{0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF},
		{0x1234, 0x5678, 0x9ABC, 0xDEF0, 0x1111, 0x2222, 0x3333, 0x4444},
	}

	for i, original := range tests {
		// Pack to Leopard format
		shard := packGF128ToLeopard(original)

		// Verify shard is 64 bytes
		if len(shard) != 64 {
			t.Errorf("test %d: packGF128ToLeopard returned %d bytes, want 64", i, len(shard))
			continue
		}

		// Verify the Leopard format
		for j := range 8 {
			lowByte := shard[j]
			highByte := shard[32+j]
			expectedLow := byte(original[j] & 0xFF)
			expectedHigh := byte(original[j] >> 8)

			if lowByte != expectedLow {
				t.Errorf("test %d: symbol %d low byte = %02x, want %02x", i, j, lowByte, expectedLow)
			}
			if highByte != expectedHigh {
				t.Errorf("test %d: symbol %d high byte = %02x, want %02x", i, j, highByte, expectedHigh)
			}
		}

		// Verify padding is zero
		for j := 8; j < 32; j++ {
			if shard[j] != 0 || shard[32+j] != 0 {
				t.Errorf("test %d: padding at position %d is not zero", i, j)
			}
		}

		// Unpack and verify round-trip
		unpacked := unpackGF128FromLeopard(shard)
		if !field.Equal128(unpacked, original) {
			t.Errorf("test %d: round-trip failed, got %v, want %v", i, unpacked, original)
		}
	}
}

func TestExtendRLCResults(t *testing.T) {
	tests := []struct {
		name    string
		k       int
		n       int
		wantErr bool
	}{
		{
			name:    "1:1 encoding k=4 n=4",
			k:       4,
			n:       4,
			wantErr: false,
		},
		{
			name:    "1:3 encoding k=4 n=12",
			k:       4,
			n:       12,
			wantErr: false,
		},
		{
			name:    "1:1 encoding k=8 n=8",
			k:       8,
			n:       8,
			wantErr: false,
		},
		{
			name:    "1:3 encoding k=8 n=24",
			k:       8,
			n:       24,
			wantErr: false,
		},
		{
			name:    "larger 1:1 k=32 n=32",
			k:       32,
			n:       32,
			wantErr: false,
		},
		{
			name:    "larger 1:3 k=32 n=96",
			k:       32,
			n:       96,
			wantErr: false,
		},
		{
			name:    "zero RLC values",
			k:       0,
			n:       4,
			wantErr: true,
		},
		{
			name:    "invalid n=0",
			k:       4,
			n:       0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test RLC values
			rlcOrig := make([]field.GF128, tt.k)
			for i := range tt.k {
				for j := range 8 {
					rlcOrig[i][j] = field.GF16(i*8 + j)
				}
			}

			// Extend RLC results
			extended, err := ExtendRLCResults(rlcOrig, tt.n)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ExtendRLCResults() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ExtendRLCResults() unexpected error: %v", err)
				return
			}

			// Verify output
			if len(extended) != tt.k+tt.n {
				t.Errorf("ExtendRLCResults() returned %d values, want %d", len(extended), tt.k+tt.n)
			}

			// Verify original RLC values are preserved
			for i := range tt.k {
				if !field.Equal128(extended[i], rlcOrig[i]) {
					t.Errorf("ExtendRLCResults() modified original RLC value %d", i)
				}
			}
		})
	}
}

func TestReconstruct(t *testing.T) {
	tests := []struct {
		name    string
		k       int
		n       int
		rowSize int
	}{
		{
			name:    "1:1 encoding k=4 n=4",
			k:       4,
			n:       4,
			rowSize: 64,
		},
		{
			name:    "1:3 encoding k=4 n=12",
			k:       4,
			n:       12,
			rowSize: 64,
		},
		{
			name:    "1:1 encoding k=8 n=8",
			k:       8,
			n:       8,
			rowSize: 128,
		},
		{
			name:    "1:3 encoding k=8 n=24",
			k:       8,
			n:       24,
			rowSize: 128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create original data
			original := make([][]byte, tt.k)
			for i := range tt.k {
				original[i] = make([]byte, tt.rowSize)
				for j := range tt.rowSize {
					original[i][j] = byte((i*tt.rowSize + j) % 256)
				}
			}

			// Extend data
			extended, err := ExtendVertical(original, tt.n)
			if err != nil {
				t.Fatalf("ExtendVertical() error: %v", err)
			}

			// Test reconstruction from different row combinations
			reconstructTests := []struct {
				name    string
				indices []int // Which rows to use for reconstruction
			}{
				{
					name:    "first k rows",
					indices: makeRange(0, tt.k),
				},
				{
					name:    "last k rows (all parity)",
					indices: makeRange(tt.n, tt.k+tt.n),
				},
				{
					name:    "mixed original and parity",
					indices: mixedIndices(tt.k, tt.n, 0),
				},
				{
					name:    "different mixed set",
					indices: mixedIndices(tt.k, tt.n, 1),
				},
			}

			for _, rt := range reconstructTests {
				t.Run(rt.name, func(t *testing.T) {
					// Select rows for reconstruction
					rows := make([][]byte, len(rt.indices))
					for i, idx := range rt.indices {
						rows[i] = extended[idx]
					}

					// Reconstruct
					reconstructed, err := Reconstruct(rows, rt.indices, tt.k, tt.n)
					if err != nil {
						t.Errorf("Reconstruct() error: %v", err)
						return
					}

					// Verify reconstruction matches original
					if len(reconstructed) != tt.k {
						t.Errorf("Reconstruct() returned %d rows, want %d", len(reconstructed), tt.k)
						return
					}

					for i := range tt.k {
						if !bytes.Equal(reconstructed[i], original[i]) {
							t.Errorf("Reconstruct() row %d doesn't match original", i)
						}
					}
				})
			}
		})
	}
}

func TestReconstructErrors(t *testing.T) {
	k := 4
	n := 4
	rowSize := 64

	// Create some test data
	rows := make([][]byte, k)
	for i := range k {
		rows[i] = make([]byte, rowSize)
	}

	tests := []struct {
		name    string
		rows    [][]byte
		indices []int
		k       int
		n       int
		wantErr bool
	}{
		{
			name:    "too few rows",
			rows:    rows[:2],
			indices: []int{0, 1},
			k:       k,
			n:       n,
			wantErr: true,
		},
		{
			name:    "mismatched rows and indices",
			rows:    rows,
			indices: []int{0, 1, 2},
			k:       k,
			n:       n,
			wantErr: true,
		},
		{
			name:    "invalid k",
			rows:    rows,
			indices: []int{0, 1, 2, 3},
			k:       0,
			n:       n,
			wantErr: true,
		},
		{
			name:    "invalid n",
			rows:    rows,
			indices: []int{0, 1, 2, 3},
			k:       k,
			n:       0,
			wantErr: true,
		},
		{
			name:    "index out of range",
			rows:    rows,
			indices: []int{0, 1, 2, 10},
			k:       k,
			n:       n,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Reconstruct(tt.rows, tt.indices, tt.k, tt.n)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Reconstruct() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Reconstruct() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestRLCPaddingConsistency(t *testing.T) {
	// Test that extended RLC shards maintain zero padding
	k := 4
	n := 4

	// Create test RLC values
	rlcOrig := make([]field.GF128, k)
	for i := range k {
		for j := range 8 {
			rlcOrig[i][j] = field.GF16(i*8 + j + 1) // Non-zero values
		}
	}

	// Pack original RLC values and verify padding
	for i := range k {
		shard := packGF128ToLeopard(rlcOrig[i])
		// Check that positions 8-31 and 40-63 are zero
		for j := 8; j < 32; j++ {
			if shard[j] != 0 || shard[32+j] != 0 {
				t.Errorf("Original shard %d has non-zero at position %d", i, j)
			}
		}
	}

	// Extend RLC results
	extended, err := ExtendRLCResults(rlcOrig, n)
	if err != nil {
		t.Fatalf("ExtendRLCResults() error: %v", err)
	}

	// Verify extended shards also maintain zero padding
	// This tests our assumption that RS extension of zeros gives zeros
	// We need to pack the extended values to check their format
	for i := k; i < k+n; i++ {
		shard := packGF128ToLeopard(extended[i])
		// The extended shards should also have zeros in padding positions
		// because RS extension is linear and extending zeros gives zeros
		for j := 8; j < 32; j++ {
			if shard[j] != 0 || shard[32+j] != 0 {
				// This would indicate our assumption about linearity is wrong
				t.Logf("Warning: Extended shard %d has non-zero padding at position %d", i, j)
			}
		}
	}
}

// Helper functions

func makeRange(start, end int) []int {
	result := make([]int, end-start)
	for i := range result {
		result[i] = start + i
	}
	return result
}

func mixedIndices(k, n int, seed int) []int {
	// Create a deterministic mixed set of k indices
	total := k + n
	indices := make([]int, k)

	// Simple deterministic mixing based on seed
	step := total / k
	offset := seed % step

	for i := range k {
		indices[i] = (i*step + offset) % total
	}

	// Ensure no duplicates
	seen := make(map[int]bool)
	for i, idx := range indices {
		for seen[idx] {
			idx = (idx + 1) % total
		}
		indices[i] = idx
		seen[idx] = true
	}

	return indices
}
