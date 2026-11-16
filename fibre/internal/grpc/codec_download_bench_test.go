package grpc

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
)

// benchDownloadResponse builds a DownloadShardResponse approximating a served
// shard: rows of rowBytes, a proof of proofDepth 32-byte segments each, and an
// RLC block of rowBytes.
func benchDownloadResponse(rows, rowBytes, proofDepth int) *types.DownloadShardResponse {
	shard := &types.BlobShard{Rlcs: make([]byte, rowBytes)}
	for i := range rows {
		row := &types.BlobRow{Index: uint32(i), Data: make([]byte, rowBytes)}
		for range proofDepth {
			row.Proof = append(row.Proof, make([]byte, 32))
		}
		shard.Rows = append(shard.Rows, row)
	}
	return &types.DownloadShardResponse{Shard: shard}
}

// BenchmarkDownloadMarshal measures marshalling a DownloadShardResponse through
// the server codec. On main this is the contiguous pooled path; with the
// download zero-copy change it is the scatter path.
func BenchmarkDownloadMarshal(b *testing.B) {
	// Production v0 shards: 32 KiB rows, up to MaxRowsPerValidator (4096) per
	// 128 MiB shard. Shape the cases that way so per-row framing/view overhead
	// is realistic, not understated by a few very large rows.
	cases := []struct {
		name           string
		rows, rowBytes int
	}{
		{"16MiB", 512, 32 << 10},
		{"128MiB", 4096, 32 << 10},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			resp := benchDownloadResponse(tc.rows, tc.rowBytes, 14)
			codec := NewServerCodec(1024, 64)
			b.SetBytes(int64(resp.Size()))
			b.ReportAllocs()
			for b.Loop() {
				bs, err := codec.Marshal(resp)
				if err != nil {
					b.Fatal(err)
				}
				bs.Free()
			}
		})
	}
}
