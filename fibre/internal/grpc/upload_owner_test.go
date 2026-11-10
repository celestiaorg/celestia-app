package grpc

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/stretchr/testify/require"
)

// Every marshaled row view keeps one owner reference until gRPC frees it; the
// wire bytes are unchanged.
func TestUploadCodecHoldsOwnerUntilViewsFreed(t *testing.T) {
	req := &types.UploadShardRequest{Shard: &types.BlobShard{
		Rows: []*types.BlobRow{
			{Index: 1, Data: bytes.Repeat([]byte{1}, 4096), Proof: [][]byte{bytes.Repeat([]byte{9}, 32)}},
			{Index: 2, Data: bytes.Repeat([]byte{2}, 64)}, // below the pooling threshold: copied
			{Index: 3, Data: bytes.Repeat([]byte{3}, 4096)},
		},
		Rlcs: bytes.Repeat([]byte{7}, 2048),
	}}
	want, err := req.Marshal()
	require.NoError(t, err)

	refs := 0
	codec := NewUploadCodec(func() bool { refs++; return true }, func() { refs-- })

	first, err := codec.Marshal(req)
	require.NoError(t, err)
	second, err := codec.Marshal(req) // a retry marshals again
	require.NoError(t, err)
	require.Equal(t, want, first.Materialize())
	require.Equal(t, 4, refs) // two pooled rows per marshal

	// The 64-byte row was copied, not aliased.
	req.Shard.Rows[1].Data[0] = 0xff
	require.Equal(t, want, first.Materialize())

	first.Free()
	require.Equal(t, 2, refs)
	second.Free()
	require.Zero(t, refs)
}

// A failed retain frees the views wrapped so far, dropping their references.
func TestUploadCodecFailsAfterRelease(t *testing.T) {
	req := &types.UploadShardRequest{Shard: &types.BlobShard{Rows: []*types.BlobRow{
		{Data: make([]byte, 4096)}, {Data: make([]byte, 4096)},
	}}}
	refs, retains := 0, 0
	codec := NewUploadCodec(func() bool {
		if retains++; retains > 1 {
			return false
		}
		refs++
		return true
	}, func() { refs-- })
	_, err := codec.Marshal(req)
	require.ErrorIs(t, err, errUploadReleased)
	require.Zero(t, refs)
}
