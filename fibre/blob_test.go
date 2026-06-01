package fibre

import (
	"testing"

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
			defer blob.Free()

			require.Equal(t, tt.dataSize, blob.DataSize())
			require.Equal(t, data, blob.Data())
		})
	}
}

func TestBlob_RowProofsAfterRelease(t *testing.T) {
	blob, err := NewBlob([]byte("test"), DefaultBlobConfigV0())
	require.NoError(t, err)

	noop := func(int, []byte, [][]byte) {}
	err = blob.RowProofs([]int{0}, noop)
	require.NoError(t, err)

	blob.Free()

	err = blob.RowProofs([]int{0}, noop)
	require.Error(t, err)
}

// TestBlob_RetainRefusesAfterRelease verifies the refcount cannot be resurrected
// once pooled storage has been released: retain returns false instead of bumping
// 0 -> 1. This is what stops a freed blob from being re-uploaded into memory the
// pool may have recycled.
func TestBlob_RetainRefusesAfterRelease(t *testing.T) {
	blob, err := NewBlob([]byte("test"), DefaultBlobConfigV0())
	require.NoError(t, err)

	// While alive, an additional non-user owner can retain.
	require.True(t, blob.retain())
	require.False(t, blob.released())

	// The user's Free drops one ref; the extra owner keeps storage alive.
	blob.Free()
	require.False(t, blob.released())
	err = blob.RowProofs([]int{0}, func(int, []byte, [][]byte) {})
	require.NoError(t, err)

	// The last owner releases -> storage returns to the pool.
	blob.release()
	require.True(t, blob.released())

	// A retain now must refuse rather than resurrect the freed blob.
	require.False(t, blob.retain())
	require.True(t, blob.released())
}
