package grpc

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/mem"
)

// A tracked request's row views keep one owner reference per marshal until
// gRPC frees the last view; the wire bytes are unchanged.
func TestTrackUploadHoldsOwnerUntilViewsFreed(t *testing.T) {
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

	codec := &pooledCodec{pool: mem.DefaultBufferPool()}
	refs, retains := 0, 0
	untrack := TrackUpload(req, func() bool { retains++; refs++; return true }, func() { refs-- })

	first, err := codec.Marshal(req)
	require.NoError(t, err)
	second, err := codec.Marshal(req) // a retry marshals again
	require.NoError(t, err)
	require.Equal(t, want, first.Materialize())
	require.Equal(t, 2, retains)
	require.Equal(t, 2, refs)

	// The 64-byte row was copied, not aliased.
	req.Shard.Rows[1].Data[0] = 0xff
	require.Equal(t, want, first.Materialize())

	first.Free()
	require.Equal(t, 1, refs)
	second.Free()
	require.Zero(t, refs)

	untrack()
	third, err := codec.Marshal(req)
	require.NoError(t, err)
	require.Equal(t, 2, retains, "untracked requests take no reference")
	third.Free()
}

func TestTrackUploadFailsAfterRelease(t *testing.T) {
	req := &types.UploadShardRequest{Shard: &types.BlobShard{Rows: []*types.BlobRow{{Data: make([]byte, 4096)}}}}
	defer TrackUpload(req, func() bool { return false }, func() { t.Fatal("released without a reference") })()
	_, err := (&pooledCodec{pool: mem.DefaultBufferPool()}).Marshal(req)
	require.ErrorIs(t, err, errUploadReleased)
}
