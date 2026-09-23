package grpc

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// overwritePool poisons returned buffers to expose aliases in decoded messages.
type overwritePool struct{ gets, puts int }

func (p *overwritePool) Get(size int) *[]byte {
	p.gets++
	buf := make([]byte, size, max(size, 4096))
	return &buf
}

func (p *overwritePool) Put(buf *[]byte) {
	p.puts++
	for i := range *buf {
		(*buf)[i] = 0xa5
	}
}

func TestCodecUploadDecodedOwnership(t *testing.T) {
	req := makeUploadShard(testMaxRows, testMaxProofs)
	req.Shard.Rows[0].Data = bytes.Repeat([]byte{7}, 4096)
	req.Promise = &types.PaymentPromise{
		ChainId: "test-chain", Height: 42, Namespace: []byte{1, 2}, BlobSize: 4096,
		Commitment: []byte{3, 4}, CreationTimestamp: time.Unix(1700000000, 0).UTC(),
		SignerPublicKey: secp256k1.PubKey{Key: []byte{5, 6}}, Signature: []byte{7, 8},
	}
	wire := marshalUploadShard(t, req)
	for _, chunked := range []bool{false, true} {
		name := "single"
		if chunked {
			name = "chunked"
		}
		t.Run(name, func(t *testing.T) {
			pool := &overwritePool{}
			codec := &pooledCodec{pool: pool, maxShardRows: testMaxRows, maxProofSegments: testMaxProofs}
			inputPool := &overwritePool{}
			var input mem.BufferSlice

			chunks := [][]byte{wire}
			if chunked {
				chunks = [][]byte{wire[:13], wire[13:]}
			}
			for _, chunk := range chunks {
				backing := inputPool.Get(len(chunk))
				copy(*backing, chunk)
				input = append(input, mem.NewBuffer(backing, inputPool))
			}
			var got types.UploadShardRequest
			require.NoError(t, codec.Unmarshal(input, &got))
			require.Equal(t, wire, input.Materialize(), "decoder must not release caller's input")
			input.Free()

			require.Equal(t, inputPool.gets, inputPool.puts, "input references must be released")
			require.Equal(t, req, &got, "decoded fields must survive recycled storage")
		})
	}
}

func TestCodecUploadErrorOwnership(t *testing.T) {
	tooManyRows := makeUploadShard(testMaxRows+1, 0)
	tooManyRows.Shard.Rlcs = make([]byte, 4096)
	// Counting decoders skip the promise; generated decoding rejects its bad field type.
	badPromise := protowire.AppendTag(nil, 1, protowire.BytesType)
	badPromise = protowire.AppendBytes(badPromise, []byte{0x08, 0x01})
	badPromise = protowire.AppendTag(badPromise, 7, protowire.BytesType)
	badPromise = protowire.AppendBytes(badPromise, make([]byte, 4096))
	for name, wire := range map[string][]byte{
		"row limit":         marshalUploadShard(t, tooManyRows),
		"generated decoder": badPromise,
	} {
		t.Run(name, func(t *testing.T) {
			pool := &overwritePool{}
			codec := NewServerCodec(testMaxRows, testMaxProofs)
			backing := pool.Get(len(wire))
			copy(*backing, wire)
			input := mem.BufferSlice{mem.NewBuffer(backing, pool)}
			require.Error(t, codec.Unmarshal(input, &types.UploadShardRequest{}))
			require.Equal(t, wire, input.Materialize())
			input.Free()
			require.Equal(t, pool.gets, pool.puts)
		})
	}
}

func BenchmarkCodecUploadDecode(b *testing.B) { benchmarkUploadDecode(b, false) }

func BenchmarkCodecUploadDecodeReuse(b *testing.B) { benchmarkUploadDecode(b, true) }

func benchmarkUploadDecode(b *testing.B, reuse bool) {
	req := makeUploadShard(148, 14)
	for _, row := range req.Shard.Rows {
		row.Data = make([]byte, 512<<10)
	}
	wire, err := req.Marshal()
	require.NoError(b, err)
	codec := NewServerCodec(4096, 14).(*pooledCodec)
	if reuse {
		codec.uploads = &uploadBuffers{limit: len(wire)}
	}
	var input mem.BufferSlice
	for offset := 0; offset < len(wire); offset += 16 << 10 {
		input = append(input, mem.SliceBuffer(wire[offset:min(offset+16<<10, len(wire))]))
	}
	b.SetBytes(int64(len(wire)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var got types.UploadShardRequest
		if err := codec.Unmarshal(input, &got); err != nil {
			b.Fatal(err)
		}
		if reuse {
			codec.uploads.release(&got)
		}
	}
}
