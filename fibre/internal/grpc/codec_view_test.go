package grpc

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

func uploadBytesField(field protowire.Number, value []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, field, protowire.BytesType), value)
}

func TestUploadViewsCanonical(t *testing.T) {
	for _, req := range []*types.UploadShardRequest{
		{},
		{Promise: &types.PaymentPromise{ChainId: "test"}},
		{Shard: &types.BlobShard{}},
		makeUploadShard(3, 2),
		{Shard: &types.BlobShard{Rows: []*types.BlobRow{{}, {Index: 1, Proof: [][]byte{{}, {1}}}}, Rlcs: []byte{2}}},
	} {
		wire := marshalUploadShard(t, req)
		var want, got types.UploadShardRequest
		require.NoError(t, want.Unmarshal(wire))
		require.True(t, unmarshalUploadViews(wire, &got))
		require.Equal(t, want, got)
	}
}

func TestUploadViewsFallback(t *testing.T) {
	promise := uploadBytesField(1, uploadBytesField(1, []byte("chain")))
	row := uploadBytesField(1, uploadBytesField(2, []byte{1, 2}))
	shard := uploadBytesField(2, row)
	rowIndex := protowire.AppendVarint([]byte{0x08}, 7)
	overflowIndex := protowire.AppendVarint([]byte{0x08}, 1<<32)
	for name, wire := range map[string][]byte{
		"unknown request field": append(bytes.Clone(shard), 0x18, 0x01),
		"duplicate shard":       append(bytes.Clone(shard), shard...),
		"duplicate promise":     append(bytes.Clone(promise), promise...),
		"reordered request":     append(bytes.Clone(shard), promise...),
		"unknown shard field":   uploadBytesField(2, []byte{0x18, 0x01}),
		"duplicate rlcs":        uploadBytesField(2, []byte{0x12, 0x01, 1, 0x12, 0x01, 2}),
		"rows after rlcs":       uploadBytesField(2, append([]byte{0x12, 0x01, 1}, row...)),
		"unknown row field":     uploadBytesField(2, uploadBytesField(1, []byte{0x20, 1})),
		"duplicate data":        uploadBytesField(2, uploadBytesField(1, []byte{0x12, 1, 1, 0x12, 1, 2})),
		"duplicate index":       uploadBytesField(2, uploadBytesField(1, append(bytes.Clone(rowIndex), rowIndex...))),
		"reordered row":         uploadBytesField(2, uploadBytesField(1, append([]byte{0x1a, 1, 1}, rowIndex...))),
		"overflow index":        uploadBytesField(2, uploadBytesField(1, overflowIndex)),
		"zero index":            uploadBytesField(2, uploadBytesField(1, []byte{0x08, 0})),
		"empty data":            uploadBytesField(2, uploadBytesField(1, []byte{0x12, 0})),
		"empty rlcs":            {0x12, 2, 0x12, 0},
		"overlong length":       {0x12, 0x80, 0},
		"overlong index":        uploadBytesField(2, uploadBytesField(1, []byte{0x08, 0x81, 0})),
		"truncated length":      {0x12, 0x80},
		"truncated field":       shard[:len(shard)-1],
		"varint overflow":       append([]byte{0x12}, bytes.Repeat([]byte{0x80}, 11)...),
		"invalid promise":       {0x0a, 2, 0x08, 1},
	} {
		t.Run(name, func(t *testing.T) {
			var got, want types.UploadShardRequest
			require.False(t, unmarshalUploadViews(wire, &got))
			require.Equal(t, types.UploadShardRequest{}, got, "fallback must not see partial fast-path state")
			gotErr := got.Unmarshal(wire)
			wantErr := want.Unmarshal(wire)
			require.Equal(t, wantErr, gotErr)
			require.Equal(t, want, got)
		})
	}
	t.Run("nonempty destination", func(t *testing.T) {
		for _, req := range []*types.UploadShardRequest{
			{Promise: &types.PaymentPromise{}}, {Shard: &types.BlobShard{}},
		} {
			before := *req
			require.False(t, unmarshalUploadViews(shard, req))
			require.Equal(t, before, *req)
		}
	})
}

func TestUploadViewsAppendDoesNotCorruptAdjacentFields(t *testing.T) {
	wire := marshalUploadShard(t, makeUploadShard(2, 2))
	var got, want types.UploadShardRequest
	require.NoError(t, want.Unmarshal(wire))
	require.True(t, unmarshalUploadViews(wire, &got))
	_ = append(got.Shard.Rows[0].Data, 99)
	_ = append(got.Shard.Rows[0].Proof[0], 99)
	_ = append(got.Shard.Rlcs, 99)
	require.Equal(t, want, got)
}

func FuzzUploadViewsParity(f *testing.F) {
	seed, err := makeUploadShard(3, 2).Marshal()
	require.NoError(f, err)
	f.Add(seed)
	f.Add([]byte{})
	f.Add([]byte{0x12, 0})
	f.Add([]byte{0x12, 0x80})
	f.Fuzz(func(t *testing.T, wire []byte) {
		if len(wire) > 64<<10 {
			t.Skip()
		}
		var got, want types.UploadShardRequest
		wantErr := want.Unmarshal(wire)
		var gotErr error
		if !unmarshalUploadViews(wire, &got) {
			gotErr = got.Unmarshal(wire)
		}
		require.Equal(t, wantErr, gotErr)
		require.Equal(t, want, got)
		codec := &pooledCodec{maxShardRows: 16, maxProofSegments: 16}
		validationErr := codec.validateUploadShard(wire)
		for _, input := range []mem.BufferSlice{
			{mem.SliceBuffer(wire)},
			{mem.SliceBuffer(wire[:len(wire)/2]), mem.SliceBuffer(wire[len(wire)/2:])},
		} {
			var decoded types.UploadShardRequest
			err := codec.Unmarshal(input, &decoded)
			if validationErr != nil {
				require.Error(t, err)
			} else {
				require.Equal(t, wantErr, err)
				require.Equal(t, want, decoded)
			}
		}
	})
}
