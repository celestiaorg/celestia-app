package fibre

import (
	"os"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"

	"github.com/stretchr/testify/require"
)

func TestLargeBlobSizeAndPadding(t *testing.T) {
	p := DefaultProtocolParams
	cfg := DefaultBlobConfigV0()
	require.Equal(t, 2<<30, p.MaxBlobSize)
	require.Equal(t, (2<<30)-blobHeaderLen, cfg.MaxDataSize)
	require.Equal(t, 512<<10, cfg.MaxRowSize)
	const quantum = 4096 * 64
	for _, size := range []int{1, quantum - blobHeaderLen, quantum - blobHeaderLen + 1, (128 << 20) - blobHeaderLen, 128 << 20, 127 << 24, cfg.MaxDataSize - 1, cfg.MaxDataSize} {
		padded := cfg.UploadSize(size)
		require.GreaterOrEqual(t, padded, size+blobHeaderLen)
		require.Less(t, padded-size-blobHeaderLen, quantum)
		require.Zero(t, padded%quantum)
		require.LessOrEqual(t, padded, p.MaxBlobSize)
		header := newBlobHeaderV0(size)
		wire := make([]byte, blobHeaderLen)
		header.marshalTo(wire)
		var decoded blobHeaderV0
		require.NoError(t, decoded.unmarshalFrom(wire))
		require.Equal(t, uint32(size), decoded.dataSize)
	}
	require.Equal(t, p.MaxBlobSize, cfg.UploadSize(cfg.MaxDataSize))
	require.Equal(t, p.MaxMessageSize(), NewClientConfigFromParams(p).MaxMessageSize)
	require.Greater(t, p.MaxMessageSize(), p.MaxShardSize())
	require.Less(t, uint64(p.MaxMessageSize()), uint64(1)<<32)
	// Reject an oversized declared payload before requiring its backing bytes.
	header := newBlobHeaderV0(cfg.MaxDataSize + 1)
	wire := make([]byte, blobHeaderLen)
	header.marshalTo(wire)
	_, err := header.decode(wire, cfg)
	require.ErrorContains(t, err, "exceeds maximum allowed size")
}

// BenchmarkBlob2GiB exercises the production row count and largest supported
// row size. Set FIBRE_BENCH_2GIB=1 and run with -run '^$' -bench '^BenchmarkBlob2GiB$' -benchtime=1x.
// Allow at least 26 GiB for the input, extended rows and codec workspace.
func BenchmarkBlob2GiB(b *testing.B) {
	if os.Getenv("FIBRE_BENCH_2GIB") != "1" {
		b.Skip("set FIBRE_BENCH_2GIB=1; requires at least 26 GiB of available memory")
	}
	cfg := DefaultBlobConfigV0()
	data := make([]byte, cfg.MaxDataSize)
	for i := range data {
		data[i] = byte(i*31 + i/251)
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blob, err := NewBlob(data, cfg)
		if err != nil {
			b.Fatal(err)
		}
		if blob.UploadSize() != 2<<30 {
			b.Fatal("unexpected padded size", blob.UploadSize())
		}
		blob.Free()
	}
}

func TestLargeBlobShardMessageSize(t *testing.T) {
	p := DefaultProtocolParams
	row := &types.BlobRow{Data: make([]byte, p.MaxRowSize(0)), Proof: make([][]byte, p.MerkleProofDepth())}
	for i := range row.Proof {
		row.Proof[i] = make([]byte, 32)
	}
	shard := &types.BlobShard{Rlcs: make([]byte, p.Rows*16), Rows: make([]*types.BlobRow, p.MaxRowsPerValidator())}
	for i := range shard.Rows {
		shard.Rows[i] = row
	}
	// Size walks shared row data without allocating a multi-GiB wire buffer.
	request := &types.UploadShardRequest{Shard: shard}
	require.Greater(t, request.Size(), 2<<30)
	require.Less(t, request.Size()+MaxPaymentPromiseSize, p.MaxMessageSize())
}
