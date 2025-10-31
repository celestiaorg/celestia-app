package fibre

import (
	"testing"
)

func TestBlobConfig_RowSize(t *testing.T) {
	tests := []struct {
		name         string
		dataLen      int
		originalRows int
		rowSizeMin   int
		wantRowSize  int
	}{
		{
			name:         "exact fit",
			dataLen:      64*8 - blobHeaderLen, // Exactly fits in 8 rows of 64 bytes
			originalRows: 8,
			rowSizeMin:   64,
			wantRowSize:  64,
		},
		{
			name:         "needs rounding up",
			dataLen:      100,
			originalRows: 8,
			rowSizeMin:   64,
			wantRowSize:  64, // ceil((100+5)/8) = 14, rounded up to 64
		},
		{
			name:         "small data",
			dataLen:      1,
			originalRows: 8,
			rowSizeMin:   64,
			wantRowSize:  64, // ceil((1+5)/8) = 1, rounded up to 64
		},
		{
			name:         "large data",
			dataLen:      10000,
			originalRows: 8,
			rowSizeMin:   64,
			wantRowSize:  1280, // ceil((10000+5)/8) = 1251, rounded up to 1280 (1251/64=19.5, so 20*64)
		},
		{
			name:         "different row size min",
			dataLen:      1000,
			originalRows: 4,
			rowSizeMin:   128,
			wantRowSize:  256, // ceil((1000+5)/4) = 252, rounded up to 256 (252/128=1.97, so 2*128)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := BlobConfig{
				OriginalRows: tt.originalRows,
				RowSizeMin:   tt.rowSizeMin,
			}

			rowSize := cfg.RowSize(tt.dataLen)
			if rowSize != tt.wantRowSize {
				t.Errorf("RowSize(%d) = %d, want %d", tt.dataLen, rowSize, tt.wantRowSize)
			}

			// verify row size is multiple of RowSizeMin
			if rowSize%tt.rowSizeMin != 0 {
				t.Errorf("RowSize(%d) = %d, not a multiple of %d", tt.dataLen, rowSize, tt.rowSizeMin)
			}
		})
	}
}

func TestBlobHeaderV0_EncodeToRows_DecodeFromRows(t *testing.T) {
	tests := []struct {
		name     string
		dataSize int
		cfg      BlobConfig
	}{
		{
			name:     "small data",
			dataSize: 10,
			cfg: BlobConfig{
				OriginalRows: 4,
				RowSizeMin:   64,
				MaxBlobSize:  10000,
			},
		},
		{
			name:     "medium data",
			dataSize: 500,
			cfg: BlobConfig{
				OriginalRows: 8,
				RowSizeMin:   64,
				MaxBlobSize:  10000,
			},
		},
		{
			name:     "large data",
			dataSize: 5000,
			cfg: BlobConfig{
				OriginalRows: 16,
				RowSizeMin:   64,
				MaxBlobSize:  10000,
			},
		},
		{
			name:     "single byte",
			dataSize: 1,
			cfg: BlobConfig{
				OriginalRows: 8,
				RowSizeMin:   64,
				MaxBlobSize:  10000,
			},
		},
		{
			name:     "exact multiple",
			dataSize: 64*8 - blobHeaderLen,
			cfg: BlobConfig{
				OriginalRows: 8,
				RowSizeMin:   64,
				MaxBlobSize:  10000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test data with recognizable pattern
			data := make([]byte, tt.dataSize)
			for i := range data {
				data[i] = byte(i % 256)
			}

			// Encode
			header := newBlobHeaderV0(len(data))
			rows := header.encodeToRows(data, tt.cfg)

			// Verify correct number of rows
			if len(rows) != tt.cfg.OriginalRows {
				t.Fatalf("encodeToRows() returned %d rows, want %d", len(rows), tt.cfg.OriginalRows)
			}

			// Verify all rows have same size
			expectedRowSize := tt.cfg.RowSize(len(data))
			for i, row := range rows {
				if len(row) != expectedRowSize {
					t.Errorf("row %d size = %d, want %d", i, len(row), expectedRowSize)
				}
			}

			// Decode
			var decodeHeader blobHeaderV0
			decodedData, err := decodeHeader.decodeFromRows(rows, tt.cfg)
			if err != nil {
				t.Fatalf("decodeFromRows() error = %v", err)
			}

			// Verify round-trip
			if len(decodedData) != len(data) {
				t.Errorf("decodeFromRows() data length mismatch, got %d bytes, want %d bytes", len(decodedData), len(data))
			}
			for i := range data {
				if decodedData[i] != data[i] {
					t.Errorf("decodeFromRows() data mismatch at index %d, got %d, want %d", i, decodedData[i], data[i])
					break
				}
			}

			// Verify header was decoded correctly
			if decodeHeader.dataSize != uint32(tt.dataSize) {
				t.Errorf("decodeFromRows() header.dataSize = %d, want %d", decodeHeader.dataSize, tt.dataSize)
			}
		})
	}
}
