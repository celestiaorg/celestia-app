package grpc

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc/mem"
)

// makeResp builds a DownloadShardResponse carrying `rows` rows of `rowSize`
// bytes each, with realistic ~14-segment Merkle proofs and a K*16 RLC blob,
// mirroring what a validator returns for a 32 MiB blob (K=4096, rowSize=8256).
func makeResp(rows, rowSize int) *types.DownloadShardResponse {
	shard := &types.BlobShard{
		Rows:         make([]*types.BlobRow, rows),
		Coefficients: bytes.Repeat([]byte{0xAB}, 4096*16),
	}
	for i := range rows {
		data := make([]byte, rowSize)
		for j := range data {
			data[j] = byte(i*31 + j)
		}
		proof := make([][]byte, 14)
		for k := range proof {
			proof[k] = bytes.Repeat([]byte{byte(k)}, 32)
		}
		shard.Rows[i] = &types.BlobRow{Index: uint32(i), Data: data, Proof: proof}
	}
	return &types.DownloadShardResponse{Shard: shard}
}

var sizes = []struct {
	rows, rowSize int
}{
	{32, 8256},   // small shard from one peer (~256 KiB)
	{256, 8256},  // medium shard (~2 MiB)
	{4096, 8256}, // full original set in one response (~32 MiB, worst case)
}

// BenchmarkDownloadUnmarshal_Stock measures the stock gogoproto unmarshal path
// that gRPC's default codec uses today — the per-row []byte allocations we want
// to remove.
func BenchmarkDownloadUnmarshal_Stock(b *testing.B) {
	for _, s := range sizes {
		wire, err := gogoproto.Marshal(makeResp(s.rows, s.rowSize))
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("rows=%d", s.rows), func(b *testing.B) {
			b.SetBytes(int64(len(wire)))
			b.ReportAllocs()
			for b.Loop() {
				var out types.DownloadShardResponse
				if err := gogoproto.Unmarshal(wire, &out); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDownloadUnmarshal_Arena measures the streaming arena decoder: one
// pooled buffer per response (recycled across calls), row payloads aliased into
// it. Input is fragmented into 16 KiB buffers to mirror gRPC's frame splitting.
func BenchmarkDownloadUnmarshal_Arena(b *testing.B) {
	for _, s := range sizes {
		resp := makeResp(s.rows, s.rowSize)
		wire, err := gogoproto.Marshal(resp)
		if err != nil {
			b.Fatal(err)
		}
		// Correctness gate: arena decode must match stock decode before timing.
		reply := &DownloadReply{}
		if err := decodeDownloadShardResponse(fragment(wire, 16*1024), reply); err != nil {
			b.Fatalf("arena decode: %v", err)
		}
		if got := len(reply.Resp.Shard.Rows); got != s.rows {
			b.Fatalf("row count: got %d want %d", got, s.rows)
		}
		if !bytes.Equal(reply.Resp.Shard.Rows[s.rows-1].Data, resp.Shard.Rows[s.rows-1].Data) {
			b.Fatal("last row data mismatch")
		}
		if !bytes.Equal(reply.Resp.Shard.Coefficients, resp.Shard.Coefficients) {
			b.Fatal("coefficients mismatch")
		}
		reply.Free()

		bs := fragment(wire, 16*1024)
		b.Run(fmt.Sprintf("rows=%d", s.rows), func(b *testing.B) {
			b.SetBytes(int64(len(wire)))
			b.ReportAllocs()
			for b.Loop() {
				reply := &DownloadReply{}
				if err := decodeDownloadShardResponse(bs, reply); err != nil {
					b.Fatal(err)
				}
				reply.Free() // return arena to pool, as the real lifecycle would
			}
		})
	}
}

// fragment splits wire into chunk-sized mem buffers, emulating gRPC's 16 KiB
// HTTP/2 frame fragmentation on the receive path.
func fragment(wire []byte, chunk int) mem.BufferSlice {
	var bs mem.BufferSlice
	for off := 0; off < len(wire); off += chunk {
		end := min(off+chunk, len(wire))
		seg := make([]byte, end-off)
		copy(seg, wire[off:end])
		bs = append(bs, mem.SliceBuffer(seg))
	}
	return bs
}
