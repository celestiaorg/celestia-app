package grpc

import (
	"bytes"
	"math/rand"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
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
					ChainId:           "arabica-11",
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
			bs, err := marshalUploadShardRequestScatter(tc.req)
			if err != nil {
				t.Fatalf("scatter marshal: %v", err)
			}
			scattered := bs.Materialize()
			if !bytes.Equal(canonical, scattered) {
				t.Fatalf("wire mismatch\ncanonical (%d): %x\nscattered (%d): %x",
					len(canonical), canonical, len(scattered), scattered)
			}
		})
	}
}

// FuzzScatterMarshalParity drives the scatter marshaler with random message
// shapes and asserts byte equality against gogoproto's canonical marshal.
// Catches drift if new fields are added to UploadShardRequest / BlobShard /
// BlobRow without updating codec_scatter.go.
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
		bs, err := marshalUploadShardRequestScatter(req)
		if err != nil {
			t.Fatalf("scatter marshal: %v", err)
		}
		scattered := bs.Materialize()
		if !bytes.Equal(canonical, scattered) {
			t.Fatalf("wire mismatch (seed=%d rows=%d proof=%d data=%d)\ncanonical (%d): %x\nscattered (%d): %x",
				seed, rowCount, proofPerRow, dataLen,
				len(canonical), canonical, len(scattered), scattered)
		}
	})
}
