package grpc

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"google.golang.org/grpc/mem"
)

func TestScatterMarshalWireParity(t *testing.T) {
	mkBytes := func(rng *rand.Rand, n int) []byte {
		b := make([]byte, n)
		_, _ = rng.Read(b)
		return b
	}
	rng := rand.New(rand.NewSource(1))

	cases := []struct {
		name string
		req  *types.UploadShardRequest
	}{
		{
			name: "minimal_one_row",
			req: &types.UploadShardRequest{
				Promise: &types.PaymentPromise{
					ChainId:           "mocha-4",
					Height:            42,
					Namespace:         mkBytes(rng, 29),
					BlobSize:          1024,
					Commitment:        mkBytes(rng, 32),
					CreationTimestamp: time.Unix(1700000000, 0).UTC(),
					SignerPublicKey:   secp256k1.PubKey{Key: mkBytes(rng, 33)},
					Signature:         mkBytes(rng, 64),
				},
				Shard: &types.BlobShard{
					Rows: []*types.BlobRow{{Index: 1, Data: mkBytes(rng, 64), Proof: [][]byte{mkBytes(rng, 32)}}},
					Rlcs: mkBytes(rng, 64),
				},
			},
		},
		{
			name: "many_rows_many_proof_segments",
			req: &types.UploadShardRequest{
				Promise: &types.PaymentPromise{
					ChainId:           "celestia",
					Height:            1234567,
					Namespace:         mkBytes(rng, 29),
					BlobSize:          1 << 20,
					BlobVersion:       1,
					Commitment:        mkBytes(rng, 32),
					CreationTimestamp: time.Unix(1800000000, 123).UTC(),
					SignerPublicKey:   secp256k1.PubKey{Key: mkBytes(rng, 33)},
					Signature:         mkBytes(rng, 64),
				},
				Shard: &types.BlobShard{
					Rows: func() []*types.BlobRow {
						rows := make([]*types.BlobRow, 8)
						for i := range rows {
							rows[i] = &types.BlobRow{
								Index: uint32(i),
								Data:  mkBytes(rng, 1024),
								Proof: [][]byte{mkBytes(rng, 32), mkBytes(rng, 32), mkBytes(rng, 32)},
							}
						}
						return rows
					}(),
					Rlcs: mkBytes(rng, 128),
				},
			},
		},
		{
			name: "row_with_zero_index_omitted",
			req: &types.UploadShardRequest{
				Promise: &types.PaymentPromise{
					ChainId:           "celestia",
					Height:            1,
					CreationTimestamp: time.Unix(0, 0).UTC(),
					Commitment:        mkBytes(rng, 32),
					Namespace:         mkBytes(rng, 29),
					SignerPublicKey:   secp256k1.PubKey{Key: mkBytes(rng, 33)},
					Signature:         mkBytes(rng, 64),
				},
				Shard: &types.BlobShard{Rows: []*types.BlobRow{{Index: 0, Data: mkBytes(rng, 8), Proof: [][]byte{mkBytes(rng, 32)}}}},
			},
		},
		{
			name: "row_without_data",
			req: &types.UploadShardRequest{
				Promise: &types.PaymentPromise{
					ChainId:           "mocha-4",
					Height:            10,
					Namespace:         mkBytes(rng, 29),
					Commitment:        mkBytes(rng, 32),
					CreationTimestamp: time.Unix(1700000000, 0).UTC(),
					SignerPublicKey:   secp256k1.PubKey{Key: mkBytes(rng, 33)},
					Signature:         mkBytes(rng, 64),
				},
				Shard: &types.BlobShard{
					Rows: []*types.BlobRow{{Index: 5, Proof: [][]byte{mkBytes(rng, 32), mkBytes(rng, 32)}}},
					Rlcs: mkBytes(rng, 32),
				},
			},
		},
		{
			name: "empty_shard",
			req: &types.UploadShardRequest{
				Promise: &types.PaymentPromise{ChainId: "celestia"},
				Shard:   &types.BlobShard{},
			},
		},
		{
			name: "nil_promise",
			req:  &types.UploadShardRequest{Shard: &types.BlobShard{}},
		},
		{
			name: "nil_shard",
			req:  &types.UploadShardRequest{Promise: &types.PaymentPromise{ChainId: "x"}},
		},
		{
			name: "both_nil",
			req:  &types.UploadShardRequest{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canonical, err := tc.req.Marshal()
			if err != nil {
				t.Fatalf("canonical marshal: %v", err)
			}
			bs, err := marshalUploadShardRequestScatter(tc.req, mem.DefaultBufferPool(), nil)
			if err != nil {
				t.Fatalf("scatter marshal: %v", err)
			}
			scattered := bs.Materialize()
			if !bytes.Equal(canonical, scattered) {
				t.Fatalf("wire mismatch\ncanonical (%d): %x\nscattered (%d): %x",
					len(canonical), canonical, len(scattered), scattered)
			}
			requireDownloadScatterParity(t, tc.req.Shard)
		})
	}
}

// requireDownloadScatterParity checks the download scatter marshaler against
// gogoproto for a response carrying shard.
func requireDownloadScatterParity(t *testing.T, shard *types.BlobShard) {
	t.Helper()
	resp := &types.DownloadShardResponse{Shard: shard}
	canonical, err := resp.Marshal()
	if err != nil {
		t.Fatalf("canonical download marshal: %v", err)
	}
	scattered := marshalDownloadShardResponseScatter(resp).Materialize()
	if !bytes.Equal(canonical, scattered) {
		t.Fatalf("download wire mismatch\ncanonical (%d): %x\nscattered (%d): %x",
			len(canonical), canonical, len(scattered), scattered)
	}
}

func TestScatterMarshalWireParity128MiB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 128 MiB scatter codec test in short mode")
	}

	const rowCount, rowSize = 512, 256 << 10
	rows := make([]*types.BlobRow, rowCount)
	for i := range rows {
		rows[i] = &types.BlobRow{
			Index: uint32(i),
			Data:  bytes.Repeat([]byte{byte(i), byte(i >> 8)}, rowSize/2),
			Proof: [][]byte{bytes.Repeat([]byte{byte(i)}, 32)},
		}
	}
	req := &types.UploadShardRequest{
		Promise: &types.PaymentPromise{ChainId: "scatter-test", BlobSize: rowCount * rowSize},
		Shard:   &types.BlobShard{Rows: rows, Rlcs: bytes.Repeat([]byte{0xa5}, rowSize)},
	}

	canonical, err := req.Marshal()
	if err != nil {
		t.Fatalf("canonical marshal: %v", err)
	}
	scattered, err := marshalUploadShardRequestScatter(req, mem.DefaultBufferPool(), nil)
	if err != nil {
		t.Fatalf("scatter marshal: %v", err)
	}
	defer scattered.Free()
	if scattered.Len() != len(canonical) {
		t.Fatalf("wire length mismatch: canonical %d, scattered %d", len(canonical), scattered.Len())
	}
	// Compare segments directly to avoid another 128 MiB allocation and a huge
	// hex dump on failure.
	offset := 0
	for i, segment := range scattered {
		data := segment.ReadOnlyData()
		if !bytes.Equal(data, canonical[offset:offset+len(data)]) {
			t.Fatalf("wire mismatch at segment %d (offset %d)", i, offset)
		}
		offset += len(data)
	}
}

// FuzzScatterMarshalParity drives the scatter marshaler with random message
// shapes and asserts byte equality against gogoproto's canonical marshal.
// Catches drift if new fields are added to UploadShardRequest / BlobShard /
// BlobRow without updating codec_scatter.go.
//
// WARNING: This fuzzer is a defense line against fibre scatter-codec wire-format
// drift and runs nightly in CI via scripts/test_fuzz.sh. Do NOT delete it, and
// if you rename it update the matching -fuzz line in scripts/test_fuzz.sh in
// lockstep. See https://github.com/celestiaorg/celestia-app/issues/7392.
func FuzzScatterMarshalParity(f *testing.F) {
	f.Add(int64(1), uint32(4), uint32(3), uint32(2))
	f.Add(int64(42), uint32(0), uint32(0), uint32(0))
	f.Add(int64(7), uint32(16), uint32(8), uint32(5))

	f.Fuzz(func(t *testing.T, seed int64, rowCount, proofPerRow, dataLen uint32) {
		// Clamp to keep messages reasonable.
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
			rows[i] = &types.BlobRow{
				Index: rng.Uint32() % 1024,
				Data:  mk(int(dataLen)),
				Proof: proof,
			}
		}

		req := &types.UploadShardRequest{
			Promise: &types.PaymentPromise{
				ChainId:           "fuzz",
				Height:            rng.Int63() % 1_000_000,
				Namespace:         mk(29),
				BlobSize:          rng.Uint32(),
				BlobVersion:       rng.Uint32() % 4,
				Commitment:        mk(32),
				CreationTimestamp: time.Unix(rng.Int63()%2_000_000_000, 0).UTC(),
				SignerPublicKey:   secp256k1.PubKey{Key: mk(33)},
				Signature:         mk(64),
			},
			Shard: &types.BlobShard{
				Rows: rows,
				Rlcs: mk(int(dataLen)),
			},
		}

		canonical, err := req.Marshal()
		if err != nil {
			t.Fatalf("canonical marshal: %v", err)
		}
		bs, err := marshalUploadShardRequestScatter(req, mem.DefaultBufferPool(), nil)
		if err != nil {
			t.Fatalf("scatter marshal: %v", err)
		}
		scattered := bs.Materialize()
		if !bytes.Equal(canonical, scattered) {
			t.Fatalf("wire mismatch (seed=%d rows=%d proof=%d data=%d)\ncanonical (%d): %x\nscattered (%d): %x",
				seed, rowCount, proofPerRow, dataLen,
				len(canonical), canonical, len(scattered), scattered)
		}
		// Pooled framing relies on scatterFramingSize being exact.
		framing := len(canonical) - len(req.Shard.Rlcs)
		for _, row := range rows {
			framing -= len(row.Data)
		}
		if got := scatterFramingSize(req); got != framing {
			t.Fatalf("scatterFramingSize = %d, framing is %d bytes", got, framing)
		}
		requireDownloadScatterParity(t, req.Shard)
	})
}

// TestScatterMarshalBufferCount checks that only row data and RLCs are views,
// so the buffer count stays linear in rows rather than in proof segments.
func TestScatterMarshalBufferCount(t *testing.T) {
	const rows, depth = 16, 14
	shard := &types.BlobShard{Rlcs: make([]byte, 64)}
	for i := range rows {
		row := &types.BlobRow{Index: uint32(i), Data: make([]byte, 1024)}
		for range depth {
			row.Proof = append(row.Proof, make([]byte, 32))
		}
		shard.Rows = append(shard.Rows, row)
	}
	req := &types.UploadShardRequest{Promise: &types.PaymentPromise{ChainId: "x"}, Shard: shard}

	upload, err := marshalUploadShardRequestScatter(req, mem.DefaultBufferPool(), nil)
	if err != nil {
		t.Fatal(err)
	}
	download := marshalDownloadShardResponseScatter(&types.DownloadShardResponse{Shard: shard})
	for name, bs := range map[string]mem.BufferSlice{"upload": upload, "download": download} {
		if got, limit := len(bs), 2*rows+3; got > limit {
			t.Fatalf("%s: got %d buffers, want at most %d", name, got, limit)
		}
		var aliased int
		for _, b := range bs {
			d := b.ReadOnlyData()
			for _, row := range shard.Rows {
				if len(d) > 0 && &d[0] == &row.Data[0] {
					aliased++
				}
			}
		}
		if aliased != rows {
			t.Fatalf("%s: %d row data buffers are views, want %d", name, aliased, rows)
		}
	}
}

// TestScatterCodecCoversAllFields fails when a field is added to a message the
// scatter codec hand-encodes. Update marshalUploadShardRequestScatter and
// blobRowSize/blobShardSize, then this count.
func TestScatterCodecCoversAllFields(t *testing.T) {
	for typ, want := range map[reflect.Type]int{
		reflect.TypeFor[types.UploadShardRequest]():    2,
		reflect.TypeFor[types.DownloadShardResponse](): 1,
		reflect.TypeFor[types.BlobShard]():             2,
		reflect.TypeFor[types.BlobRow]():               3,
	} {
		if got := typ.NumField(); got != want {
			t.Errorf("%s has %d fields, scatter codec handles %d", typ.Name(), got, want)
		}
	}
}

func scatterTestRequest(rows, depth int) *types.UploadShardRequest {
	shard := &types.BlobShard{Rlcs: make([]byte, 64)}
	for i := range rows {
		row := &types.BlobRow{Index: uint32(i), Data: make([]byte, 1024)}
		for range depth {
			row.Proof = append(row.Proof, make([]byte, 32))
		}
		shard.Rows = append(shard.Rows, row)
	}
	return &types.UploadShardRequest{Promise: &types.PaymentPromise{ChainId: "x"}, Shard: shard}
}

// TestScatterMarshalAllocsConstant checks that allocations do not grow with
// the number of buffer views: views are not boxed one by one. The requests
// carry no proofs so that framing stays below the pooling threshold: pooled
// framing slices are recycled through a sync.Pool, which is not deterministic.
func TestScatterMarshalAllocsConstant(t *testing.T) {
	allocs := func(rows int) float64 {
		req := scatterTestRequest(rows, 0)
		if !mem.IsBelowBufferPoolingThreshold(scatterFramingSize(req)) {
			t.Fatalf("framing of %d rows is pooled", rows)
		}
		return testing.AllocsPerRun(20, func() {
			if _, err := marshalUploadShardRequestScatter(req, mem.DefaultBufferPool(), nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	if small, large := allocs(8), allocs(96); large != small {
		t.Fatalf("allocs grew with rows: %v at 8 rows, %v at 96 rows", small, large)
	}
}

func BenchmarkScatterMarshal(b *testing.B) {
	req := scatterTestRequest(168, 14)
	b.ReportAllocs()
	pool := mem.DefaultBufferPool()
	for b.Loop() {
		bs, err := marshalUploadShardRequestScatter(req, pool, nil)
		if err != nil {
			b.Fatal(err)
		}
		bs.Free()
	}
}

// countingPool counts buffers taken from and returned to a real pool.
type countingPool struct {
	mem.BufferPool
	gets, puts int
}

func (p *countingPool) Get(n int) *[]byte { p.gets++; return p.BufferPool.Get(n) }
func (p *countingPool) Put(b *[]byte)     { p.puts++; p.BufferPool.Put(b) }

// Pooled framing returns to the pool once gRPC frees the last framing slice.
func TestScatterMarshalPooledFraming(t *testing.T) {
	req := scatterTestRequest(168, 14)
	pool := &countingPool{BufferPool: mem.DefaultBufferPool()}
	bs, err := marshalUploadShardRequestScatter(req, pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pool.gets != 1 || pool.puts != 0 {
		t.Fatalf("before Free: %d gets, %d puts; want 1, 0", pool.gets, pool.puts)
	}
	bs.Free()
	if pool.puts != 1 {
		t.Fatalf("after Free: %d puts; want 1", pool.puts)
	}
}
