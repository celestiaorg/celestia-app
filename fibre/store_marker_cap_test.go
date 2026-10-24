package fibre

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

func TestShardMarkerSizePlausibility(t *testing.T) {
	// 4096 rows of 32 KiB data and 14 proof segments, plus 4096 RLCs.
	const maximum int64 = 12 + 4096*16 + 4096*(12+32768+14*(4+32))
	require.Equal(t, maximum, maxPlausibleShardSize)
	for _, backend := range []shardBackendTag{localBackendTag, objectBackendTag} {
		for _, size := range []int64{1, maximum - 1, maximum} {
			gotBackend, gotSize, err := decodeShardMarkerBackend(encodeShardMarkerForBackend(backend, size))
			require.NoError(t, err)
			require.Equal(t, backend, gotBackend)
			require.Equal(t, size, gotSize)
		}
		for _, size := range []int64{maximum + 1, 1 << 62} {
			_, _, err := decodeShardMarkerBackend(encodeShardMarkerForBackend(backend, size))
			require.ErrorIs(t, err, ErrStoreIntegrity)
		}
	}
}

func TestSizeRejectsImplausibleMarkerSize(t *testing.T) {
	store := newMarkerTestStore(t)
	require.NoError(t, store.db.Set(shardKey(Commitment{1}, []byte{1}), encodeShardMarkerForBackend(localBackendTag, 1<<62), pebbledb.NoSync))
	size, err := store.Size(t.Context())
	require.ErrorIs(t, err, ErrStoreIntegrity)
	require.Zero(t, size)
}

func TestMaxShardBinarySizeMatchesLargestShard(t *testing.T) {
	p := DefaultProtocolParams
	p.Rows = 16
	p.MaxBlobSize = p.Rows * 2 * p.MinRowSize

	shard := &types.BlobShard{Rlcs: make([]byte, p.Rows*field.GF128Size)}
	for i := range p.MaxRowsPerValidator() {
		row := &types.BlobRow{Index: uint32(i), Data: make([]byte, p.MaxRowSize(0))}
		for range p.MerkleProofDepth() {
			row.Proof = append(row.Proof, make([]byte, merkle.NodeSize))
		}
		shard.Rows = append(shard.Rows, row)
	}
	require.Equal(t, shardBinarySize(shard), maxShardBinarySize(p))
}
