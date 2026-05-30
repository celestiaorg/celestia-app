package grpc

import (
	"bytes"
	"math/rand"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
)

// TestUploadArenaDecodeRoundTrip checks the server-side arena decoder of
// UploadShardRequest reproduces what gogoproto.Unmarshal would, across shard
// shapes and the nil-promise / nil-shard edge cases, over a fragmented buffer.
func TestUploadArenaDecodeRoundTrip(t *testing.T) {
	mkBytes := func(rng *rand.Rand, n int) []byte {
		b := make([]byte, n)
		_, _ = rng.Read(b)
		return b
	}
	rng := rand.New(rand.NewSource(4))

	promise := func() *types.PaymentPromise {
		return &types.PaymentPromise{
			ChainId:           "mocha-4",
			Height:            42,
			Namespace:         mkBytes(rng, 29),
			BlobSize:          1024,
			Commitment:        mkBytes(rng, 32),
			CreationTimestamp: time.Unix(1700000000, 0).UTC(),
			SignerPublicKey:   secp256k1.PubKey{Key: mkBytes(rng, 33)},
			Signature:         mkBytes(rng, 64),
		}
	}

	cases := []struct {
		name string
		req  *types.UploadShardRequest
	}{
		{"nil_both", &types.UploadShardRequest{}},
		{"nil_shard", &types.UploadShardRequest{Promise: promise()}},
		{"nil_promise", &types.UploadShardRequest{Shard: &types.BlobShard{
			Rows: []*types.BlobRow{{Index: 1, Data: mkBytes(rng, 64), Proof: [][]byte{mkBytes(rng, 32)}}},
		}}},
		{"full", &types.UploadShardRequest{
			Promise: promise(),
			Shard: &types.BlobShard{
				Rows: func() []*types.BlobRow {
					rows := make([]*types.BlobRow, 8)
					for i := range rows {
						rows[i] = &types.BlobRow{Index: uint32(i), Data: mkBytes(rng, 1024), Proof: [][]byte{mkBytes(rng, 32), mkBytes(rng, 32)}}
					}
					return rows
				}(),
				Coefficients: mkBytes(rng, 128),
				Root:         mkBytes(rng, 32),
			},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := tc.req.Marshal()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got types.UploadShardRequest
			if err := decodeUploadShardRequest(fragment(wire, 16), &got); err != nil {
				t.Fatalf("arena decode: %v", err)
			}
			// Re-marshal must reproduce the canonical wire bytes (covers both
			// the promise and the shard, including the RLC fields).
			gotWire, err := got.Marshal()
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if !bytes.Equal(wire, gotWire) {
				t.Fatalf("round-trip mismatch\nwant (%d): %x\ngot  (%d): %x",
					len(wire), wire, len(gotWire), gotWire)
			}
			assertShardEqual(t, tc.req.Shard, got.Shard)
		})
	}
}

// FuzzUploadArenaDecodeParity asserts the upload arena decoder round-trips to
// the canonical wire bytes for random message shapes over a fragmented buffer.
func FuzzUploadArenaDecodeParity(f *testing.F) {
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
		req := &types.UploadShardRequest{
			Promise: &types.PaymentPromise{
				ChainId:           "fuzz",
				Height:            rng.Int63() % 1_000_000,
				Namespace:         mk(29),
				BlobSize:          rng.Uint32(),
				Commitment:        mk(32),
				CreationTimestamp: time.Unix(rng.Int63()%2_000_000_000, 0).UTC(),
				SignerPublicKey:   secp256k1.PubKey{Key: mk(33)},
				Signature:         mk(64),
			},
			Shard: &types.BlobShard{Rows: rows, Coefficients: mk(int(dataLen)), Root: mk(32)},
		}

		wire, err := req.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got types.UploadShardRequest
		if err := decodeUploadShardRequest(fragment(wire, 16), &got); err != nil {
			t.Fatalf("arena decode (seed=%d): %v", seed, err)
		}
		gotWire, err := got.Marshal()
		if err != nil {
			t.Fatalf("re-marshal: %v", err)
		}
		if !bytes.Equal(wire, gotWire) {
			t.Fatalf("round-trip mismatch (seed=%d)\nwant (%d): %x\ngot (%d): %x",
				seed, len(wire), wire, len(gotWire), gotWire)
		}
	})
}
