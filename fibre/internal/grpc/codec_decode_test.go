package grpc

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// makeUploadShard builds an UploadShardRequest with rows rows of proofsPerRow
// proof segments each. Rows carry data and the shard carries RLCs so the
// cardinality scan's skip paths are exercised; only the repeated-field counts
// matter to the limits.
func makeUploadShard(rows, proofsPerRow int) *types.UploadShardRequest {
	proof := make([][]byte, proofsPerRow)
	for i := range proof {
		proof[i] = make([]byte, 32)
	}
	blobRows := make([]*types.BlobRow, rows)
	for i := range blobRows {
		blobRows[i] = &types.BlobRow{Index: uint32(i), Data: []byte{0xff}, Proof: proof}
	}
	return &types.UploadShardRequest{Shard: &types.BlobShard{Rows: blobRows, Rlcs: make([]byte, 16)}}
}

func marshalUploadShard(t *testing.T, req *types.UploadShardRequest) []byte {
	t.Helper()
	buf, err := req.Marshal()
	require.NoError(t, err)
	return buf
}

func TestValidateUploadShardCardinality(t *testing.T) {
	tests := []struct {
		name         string
		rows         int
		proofsPerRow int
		wantErr      string
	}{
		{name: "empty request"},
		{name: "at row and proof limits", rows: types.MaxBlobRowsPerShard, proofsPerRow: types.MaxProofSegmentsPerRow},
		{name: "one row over limit", rows: types.MaxBlobRowsPerShard + 1, proofsPerRow: 1, wantErr: "rows"},
		{name: "one proof over limit", rows: 1, proofsPerRow: types.MaxProofSegmentsPerRow + 1, wantErr: "proof segments"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUploadShardCardinality(marshalUploadShard(t, makeUploadShard(tc.rows, tc.proofsPerRow)))
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}

	t.Run("only last row over proof limit", func(t *testing.T) {
		req := makeUploadShard(3, 1)
		req.Shard.Rows[2].Proof = make([][]byte, types.MaxProofSegmentsPerRow+1)
		err := validateUploadShardCardinality(marshalUploadShard(t, req))
		require.ErrorContains(t, err, "proof segments")
	})

	t.Run("unknown trailing field skipped", func(t *testing.T) {
		buf := marshalUploadShard(t, makeUploadShard(1, 1))
		buf = protowire.AppendTag(buf, 7, protowire.VarintType)
		buf = protowire.AppendVarint(buf, 42)
		require.NoError(t, validateUploadShardCardinality(buf))
	})

	t.Run("truncated bytes rejected", func(t *testing.T) {
		buf := marshalUploadShard(t, makeUploadShard(2, 2))
		require.Error(t, validateUploadShardCardinality(buf[:len(buf)-1]))
	})

	t.Run("malformed tag rejected", func(t *testing.T) {
		require.Error(t, validateUploadShardCardinality([]byte{0x80}))
	})

	t.Run("tag without value rejected", func(t *testing.T) {
		require.Error(t, validateUploadShardCardinality(protowire.AppendTag(nil, 7, protowire.VarintType)))
	})
}

// TestCodecUnmarshalBoundsCardinality verifies the codec rejects an
// over-cardinality UploadShardRequest before the generated decoder allocates,
// and that a maximal valid request round-trips through the codec's own
// scatter marshaler.
func TestCodecUnmarshalBoundsCardinality(t *testing.T) {
	codec := &pooledCodec{pool: mem.DefaultBufferPool()}

	t.Run("rejects amplified request", func(t *testing.T) {
		buf := marshalUploadShard(t, makeUploadShard(types.MaxBlobRowsPerShard+1, 0))
		err := codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(buf)}, &types.UploadShardRequest{})
		require.ErrorContains(t, err, "rows")
	})

	t.Run("round-trips a valid request", func(t *testing.T) {
		wire, err := codec.Marshal(makeUploadShard(2, types.MaxProofSegmentsPerRow))
		require.NoError(t, err)

		var got types.UploadShardRequest
		require.NoError(t, codec.Unmarshal(wire, &got))
		require.Len(t, got.Shard.Rows, 2)
		require.Len(t, got.Shard.Rows[1].Proof, types.MaxProofSegmentsPerRow)
	})
}
