package fibre

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/stretchr/testify/require"
)

func TestBlobHeaderV0_MarshalRoundTrip(t *testing.T) {
	sizes := []int{1, 10, 500, 5000, 1024, 1 << 20}

	for _, size := range sizes {
		h := newBlobHeaderV0(size)
		buf := make([]byte, blobHeaderLen)
		h.marshalTo(buf)

		var got blobHeaderV0
		require.NoError(t, got.unmarshalFrom(buf))
		require.Equal(t, uint32(size), got.dataSize)
	}
}

func TestNewBlob_EncodeDecodeRoundTrip(t *testing.T) {
	cfg := DefaultBlobConfigV0()

	tests := []struct {
		name     string
		dataSize int
	}{
		{"single byte", 1},
		{"small", 10},
		{"medium", 500},
		{"large", 5000},
		{"1 KiB", 1024},
		{"1 MiB", 1 << 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, tt.dataSize)
			for i := range data {
				data[i] = byte(i % 256)
			}

			blob, err := NewBlob(data, cfg)
			require.NoError(t, err)
			defer blob.asm.Free(nil)

			require.Equal(t, tt.dataSize, blob.DataSize())
			require.Equal(t, data, blob.Data())
		})
	}
}

func TestBlob_Reconstruct(t *testing.T) {
	testData := []byte("test erasure coding reconstruction")
	cfg := DefaultBlobConfigV0()

	blob, err := NewBlob(testData, cfg)
	require.NoError(t, err)
	defer blob.asm.Free(nil)

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
			require.NoError(t, reconstructBlob.VerifyRow(row))
			require.True(t, reconstructBlob.SetRow(row))
		}

		require.NoError(t, reconstructBlob.Reconstruct())
		require.Equal(t, testData, reconstructBlob.Data())
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

func TestBlob_RowAfterRelease(t *testing.T) {
	blob, err := NewBlob([]byte("test"), DefaultBlobConfigV0())
	require.NoError(t, err)

	_, err = blob.Row(0)
	require.NoError(t, err)

	blob.asm.Free(nil)

	_, err = blob.Row(0)
	require.Error(t, err)
}

func TestBlob_RowAfterPartialRelease(t *testing.T) {
	cfg := DefaultBlobConfigV0()
	blob, err := NewBlob([]byte("test"), cfg)
	require.NoError(t, err)
	defer blob.asm.Free(nil)

	// Release a single parity row; other rows must remain available.
	released := cfg.OriginalRows + 1
	blob.asm.Free([]int{released})

	_, err = blob.Row(released)
	require.Error(t, err, "released parity row should error")

	_, err = blob.Row(cfg.OriginalRows)
	require.NoError(t, err, "untouched parity row should succeed")

	_, err = blob.Row(0)
	require.NoError(t, err, "original rows should always succeed")
}
