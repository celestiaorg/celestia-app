package grpc

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
)

// assertShardEqual fails the test unless want and got carry identical shard
// contents (treating nil and empty byte slices as equal).
func assertShardEqual(t *testing.T, want, got *types.BlobShard) {
	t.Helper()
	if (want == nil) != (got == nil) {
		t.Fatalf("shard nil mismatch: want nil=%v got nil=%v", want == nil, got == nil)
	}
	if want == nil {
		return
	}
	if len(want.Rows) != len(got.Rows) {
		t.Fatalf("rows: want %d got %d", len(want.Rows), len(got.Rows))
	}
	for i := range want.Rows {
		if want.Rows[i].Index != got.Rows[i].Index {
			t.Fatalf("row %d index: want %d got %d", i, want.Rows[i].Index, got.Rows[i].Index)
		}
		if !bytes.Equal(want.Rows[i].Data, got.Rows[i].Data) {
			t.Fatalf("row %d data mismatch", i)
		}
		if len(want.Rows[i].Proof) != len(got.Rows[i].Proof) {
			t.Fatalf("row %d proof segs: want %d got %d", i, len(want.Rows[i].Proof), len(got.Rows[i].Proof))
		}
		for j := range want.Rows[i].Proof {
			if !bytes.Equal(want.Rows[i].Proof[j], got.Rows[i].Proof[j]) {
				t.Fatalf("row %d proof %d mismatch", i, j)
			}
		}
	}
	if !bytes.Equal(want.Coefficients, got.Coefficients) {
		t.Fatalf("coefficients mismatch")
	}
	if !bytes.Equal(want.Root, got.Root) {
		t.Fatalf("root mismatch")
	}
}

// TestDownloadArenaDecodeRoundTrip checks the receive-side arena decoder
// reproduces the original shard, across shard shapes and proto3 edge cases,
// decoding over a heavily fragmented buffer to exercise the streaming walk.
func TestDownloadArenaDecodeRoundTrip(t *testing.T) {
	mkBytes := func(rng *rand.Rand, n int) []byte {
		b := make([]byte, n)
		_, _ = rng.Read(b)
		return b
	}
	rng := rand.New(rand.NewSource(3))

	cases := []struct {
		name string
		resp *types.DownloadShardResponse
	}{
		{"nil_shard", &types.DownloadShardResponse{}},
		{"empty_shard", &types.DownloadShardResponse{Shard: &types.BlobShard{}}},
		{"one_row", &types.DownloadShardResponse{Shard: &types.BlobShard{
			Rows:         []*types.BlobRow{{Index: 1, Data: mkBytes(rng, 64), Proof: [][]byte{mkBytes(rng, 32)}}},
			Coefficients: mkBytes(rng, 64),
			Root:         mkBytes(rng, 32),
		}}},
		{"zero_index_omitted", &types.DownloadShardResponse{Shard: &types.BlobShard{
			Rows: []*types.BlobRow{{Index: 0, Data: mkBytes(rng, 8), Proof: [][]byte{mkBytes(rng, 32)}}},
		}}},
		{"row_without_data", &types.DownloadShardResponse{Shard: &types.BlobShard{
			Rows: []*types.BlobRow{{Index: 5, Proof: [][]byte{mkBytes(rng, 32), mkBytes(rng, 32)}}},
			Root: mkBytes(rng, 32),
		}}},
		{"many_rows", &types.DownloadShardResponse{Shard: &types.BlobShard{
			Rows: func() []*types.BlobRow {
				rows := make([]*types.BlobRow, 8)
				for i := range rows {
					rows[i] = &types.BlobRow{Index: uint32(i), Data: mkBytes(rng, 1024), Proof: [][]byte{mkBytes(rng, 32), mkBytes(rng, 32)}}
				}
				return rows
			}(),
			Coefficients: mkBytes(rng, 128),
		}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := tc.resp.Marshal()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			reply := &DownloadReply{}
			if err := decodeDownloadShardResponse(fragment(wire, 16), reply); err != nil {
				t.Fatalf("arena decode: %v", err)
			}
			assertShardEqual(t, tc.resp.Shard, reply.Resp.Shard)
			reply.Free()
		})
	}
}

// FuzzDownloadArenaDecodeParity asserts the arena decoder produces exactly what
// gogoproto.Unmarshal would, for random shard shapes over a fragmented buffer.
func FuzzDownloadArenaDecodeParity(f *testing.F) {
	f.Add(int64(1), uint32(4), uint32(3), uint32(64))
	f.Add(int64(42), uint32(0), uint32(0), uint32(0))
	f.Add(int64(7), uint32(16), uint32(8), uint32(200))

	f.Fuzz(func(t *testing.T, seed int64, rowCount, proofPerRow, dataLen uint32) {
		rowCount %= 16
		proofPerRow %= 8
		dataLen %= 256

		rng := rand.New(rand.NewSource(seed))
		mk := func(n int) []byte {
			b := make([]byte, n)
			_, _ = rng.Read(b)
			return b
		}

		rows := make([]*types.BlobRow, rowCount)
		for i := range rows {
			proof := make([][]byte, proofPerRow)
			for j := range proof {
				proof[j] = mk(32)
			}
			rows[i] = &types.BlobRow{Index: rng.Uint32() % 1024, Data: mk(int(dataLen)), Proof: proof}
		}
		resp := &types.DownloadShardResponse{Shard: &types.BlobShard{
			Rows:         rows,
			Coefficients: mk(int(dataLen)),
			Root:         mk(32),
		}}

		wire, err := resp.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var want types.DownloadShardResponse
		if err := want.Unmarshal(wire); err != nil {
			t.Fatalf("gogoproto unmarshal: %v", err)
		}
		reply := &DownloadReply{}
		if err := decodeDownloadShardResponse(fragment(wire, 16), reply); err != nil {
			t.Fatalf("arena decode (seed=%d): %v", seed, err)
		}
		assertShardEqual(t, want.Shard, reply.Resp.Shard)
		reply.Free()
	})
}
