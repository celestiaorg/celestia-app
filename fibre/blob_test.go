package fibre

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/stretchr/testify/require"
)

func TestBlobHeaderV0_EncodeToRows_DecodeFromRows(t *testing.T) {
	cfg := DefaultBlobConfigV0()

	tests := []struct {
		name     string
		dataSize int
	}{
		{"single byte", 1},
		{"small data", 10},
		{"medium data", 500},
		{"large data", 5000},
		{"1 KiB", 1024},
		{"1 MiB", 1024 * 1024},
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
			rows := header.encodeToRows(data, cfg)

			// Verify correct number of rows
			if len(rows) != cfg.OriginalRows {
				t.Fatalf("encodeToRows() returned %d rows, want %d", len(rows), cfg.OriginalRows)
			}

			// Verify all rows have same size
			expectedRowSize := cfg.RowSize(len(data))
			for i, row := range rows {
				if len(row) != expectedRowSize {
					t.Errorf("row %d size = %d, want %d", i, len(row), expectedRowSize)
				}
			}

			// Decode
			var decodeHeader blobHeaderV0
			decodedData, err := decodeHeader.decodeFromRows(rows, cfg)
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

func TestBlob_Reconstruct(t *testing.T) {
	testData := []byte("test erasure coding reconstruction")
	cfg := DefaultBlobConfigV0()

	blob, err := NewBlob(testData, cfg)
	require.NoError(t, err)

	totalRows := cfg.OriginalRows + cfg.ParityRows
	allRows := make([]*rsema1d.RowInclusionProof, totalRows)
	for i := range totalRows {
		row, err := blob.Row(i)
		require.NoError(t, err)
		allRows[i] = row
	}

	testReconstruct := func(t *testing.T, rows []*rsema1d.RowInclusionProof) {
		reconstructBlob, err := NewEmptyBlob(blob.ID())
		require.NoError(t, err)

		for _, row := range rows {
			err = reconstructBlob.VerifyRow(row)
			require.NoError(t, err)
			require.True(t, reconstructBlob.SetRow(row))
		}

		err = reconstructBlob.Reconstruct()
		require.NoError(t, err)

		reconstructedData := reconstructBlob.Data()
		require.Equal(t, testData, reconstructedData)
	}

	t.Run("FirstKRows", func(t *testing.T) {
		testReconstruct(t, allRows[:cfg.OriginalRows])
	})

	t.Run("LastKRows", func(t *testing.T) {
		testReconstruct(t, allRows[totalRows-cfg.OriginalRows:])
	})

	t.Run("MixedRows", func(t *testing.T) {
		mixedRows := make([]*rsema1d.RowInclusionProof, 0, cfg.OriginalRows)
		for i := 0; i < cfg.OriginalRows; i++ {
			idx := i * 2
			if idx < totalRows {
				mixedRows = append(mixedRows, allRows[idx])
			}
		}
		testReconstruct(t, mixedRows)
	})
}
