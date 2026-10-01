package fibre

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func patternData(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(uint32(i) * 2654435761 >> 24)
	}
	return data
}

func TestVerifyCommitment(t *testing.T) {
	cfg := DefaultBlobConfigV0()
	sizes := []int{1, 5000, cfg.OriginalRows*64 - blobHeaderLen, cfg.OriginalRows*64 - blobHeaderLen + 1, 1 << 20}
	if !testing.Short() {
		sizes = append(sizes, 32<<20-blobHeaderLen)
	}
	for _, size := range sizes {
		data := patternData(size)
		blob, err := NewBlob(bytes.Clone(data), cfg)
		require.NoError(t, err)
		siblings, err := blob.RowRootSiblings()
		require.NoError(t, err)
		require.Len(t, siblings, 2)

		indices := make([]int, cfg.OriginalRows)
		for i := range indices {
			indices[i] = i
		}
		rows := originalRows(data, blob.RowSize(), cfg.OriginalRows)
		require.NoError(t, blob.RowProofs(indices, func(i int, row []byte, _ [][]byte) {
			require.Equal(t, row, rows[i], "size %d row %d", size, i)
		}))
		id := blob.ID()
		blob.Free()

		require.NoError(t, VerifyCommitment(data, id, siblings), "size %d", size)

		tampered := bytes.Clone(data)
		tampered[len(tampered)-1] ^= 1
		require.Error(t, VerifyCommitment(tampered, id, siblings))
		require.Error(t, VerifyCommitment(data, id, [][]byte{siblings[0]}))
		badSibling := [][]byte{siblings[0], bytes.Clone(siblings[1])}
		badSibling[1][0] ^= 1
		require.Error(t, VerifyCommitment(data, id, badSibling))
		require.Error(t, VerifyCommitment(data, id, [][]byte{siblings[1], siblings[0]}))
		if size > 1 {
			require.Error(t, VerifyCommitment(data[:size-1], id, siblings))
		}
	}
}

func TestVerifyCommitmentRejectsBadInput(t *testing.T) {
	sibling := make([]byte, 32)
	id := NewBlobID(0, Commitment{})
	require.Error(t, VerifyCommitment(nil, id, [][]byte{sibling, sibling}))
	require.Error(t, VerifyCommitment([]byte{1}, NewBlobID(1, Commitment{}), [][]byte{sibling, sibling}))
	require.Error(t, VerifyCommitment([]byte{1}, id, [][]byte{sibling, sibling, sibling}))
	require.Error(t, VerifyCommitment([]byte{1}, id, [][]byte{sibling, sibling[:31]}))
	require.Error(t, VerifyCommitment([]byte{1}, BlobID{0}, [][]byte{sibling, sibling}))
}

func BenchmarkVerifyCommitment(b *testing.B) {
	cfg := DefaultBlobConfigV0()
	for _, size := range []int{32<<20 - blobHeaderLen, 128<<20 - blobHeaderLen} {
		data := patternData(size)
		blob, err := NewBlob(bytes.Clone(data), cfg)
		require.NoError(b, err)
		siblings, err := blob.RowRootSiblings()
		require.NoError(b, err)
		id := blob.ID()
		blob.Free()
		b.Run(byteSize(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for range b.N {
				require.NoError(b, VerifyCommitment(data, id, siblings))
			}
		})
	}
}

func byteSize(n int) string {
	if n >= 100<<20 {
		return "128MiB"
	}
	return "32MiB"
}
