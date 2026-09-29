package grpc

import (
	"bytes"
	"testing"
	"unsafe"

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

func makeDownloadShard(rows, proofsPerRow int) *types.DownloadShardResponse {
	return &types.DownloadShardResponse{Shard: makeUploadShard(rows, proofsPerRow).Shard}
}

func marshalDownloadShard(t *testing.T, resp *types.DownloadShardResponse) []byte {
	t.Helper()
	buf, err := resp.Marshal()
	require.NoError(t, err)
	return buf
}

func TestDownloadViewsCanonical(t *testing.T) {
	for _, resp := range []*types.DownloadShardResponse{
		{},
		{Shard: &types.BlobShard{}},
		{Shard: &types.BlobShard{Rlcs: []byte{2}}},
		makeDownloadShard(3, 2),
		makeDownloadShard(1, 0),
		{Shard: &types.BlobShard{Rows: []*types.BlobRow{{}, {Index: 1, Proof: [][]byte{{}, {1}}}}, Rlcs: []byte{2}}},
	} {
		wire := marshalDownloadShard(t, resp)
		var want, got types.DownloadShardResponse
		require.NoError(t, want.Unmarshal(wire))
		require.True(t, unmarshalDownloadViews(wire, &got))
		require.Equal(t, want, got)
	}
}

func TestDownloadViewsFallback(t *testing.T) {
	row := uploadBytesField(1, uploadBytesField(2, []byte{1, 2}))
	shard := uploadBytesField(1, row)
	for name, wire := range map[string][]byte{
		"unknown response field": append(bytes.Clone(shard), 0x10, 0x01),
		"duplicate shard":        append(bytes.Clone(shard), shard...),
		"unknown shard field":    uploadBytesField(1, []byte{0x18, 0x01}),
		"rows after rlcs":        uploadBytesField(1, append([]byte{0x12, 0x01, 1}, row...)),
		"unknown row field":      uploadBytesField(1, uploadBytesField(1, []byte{0x20, 1})),
		"zero index":             uploadBytesField(1, uploadBytesField(1, []byte{0x08, 0})),
		"empty data":             uploadBytesField(1, uploadBytesField(1, []byte{0x12, 0})),
		"empty rlcs":             {0x0a, 2, 0x12, 0},
		"truncated length":       {0x0a, 0x80},
		"truncated field":        shard[:len(shard)-1],
		"varint overflow":        append([]byte{0x0a}, bytes.Repeat([]byte{0x80}, 11)...),
	} {
		t.Run(name, func(t *testing.T) {
			var got, want types.DownloadShardResponse
			require.False(t, unmarshalDownloadViews(wire, &got))
			require.Equal(t, types.DownloadShardResponse{}, got, "fallback must not see partial fast-path state")
			gotErr := got.Unmarshal(wire)
			wantErr := want.Unmarshal(wire)
			require.Equal(t, wantErr, gotErr)
			require.Equal(t, want, got)
		})
	}
	t.Run("nonempty destination", func(t *testing.T) {
		resp := &types.DownloadShardResponse{Shard: &types.BlobShard{}}
		before := *resp
		require.False(t, unmarshalDownloadViews(shard, resp))
		require.Equal(t, before, *resp)
	})
}

// TestCodecDownloadDecodeAliases checks that the client decode does not copy
// row data out of the materialized buffer.
func TestCodecDownloadDecodeAliases(t *testing.T) {
	wire := marshalDownloadShard(t, makeDownloadShard(4, 2))
	codec := NewDownloadCodec(4, 2)
	var got types.DownloadShardResponse
	require.NoError(t, codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(wire)}, &got))

	// Every slice must point into one span no larger than the wire message.
	lo, hi := uintptr(1<<63), uintptr(0)
	span := func(b []byte) {
		p := uintptr(unsafe.Pointer(unsafe.SliceData(b)))
		lo, hi = min(lo, p), max(hi, p+uintptr(len(b)))
	}
	for _, row := range got.Shard.Rows {
		span(row.Data)
		for _, proof := range row.Proof {
			span(proof)
		}
	}
	span(got.Shard.Rlcs)
	require.LessOrEqual(t, hi-lo, uintptr(len(wire)), "rows must alias one materialized buffer")
}

func TestCodecDownloadLimits(t *testing.T) {
	codec := NewDownloadCodec(testMaxRows, testMaxProofs)
	unmarshal := func(wire []byte) error {
		return codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(wire)}, &types.DownloadShardResponse{})
	}
	require.NoError(t, unmarshal(marshalDownloadShard(t, makeDownloadShard(testMaxRows, testMaxProofs))))
	require.ErrorContains(t, unmarshal(marshalDownloadShard(t, makeDownloadShard(testMaxRows+1, 0))), "rows")
	require.ErrorContains(t, unmarshal(marshalDownloadShard(t, makeDownloadShard(1, testMaxProofs+1))), "proof segments")

	// Repeated shard fields merge, so their rows count together.
	one := marshalDownloadShard(t, makeDownloadShard(testMaxRows, 0))
	require.ErrorContains(t, unmarshal(append(bytes.Clone(one), uploadBytesField(1, uploadBytesField(1, nil))...)), "rows")

	// Unknown fields before the shard are skipped, not miscounted.
	require.NoError(t, unmarshal(append([]byte{0x10, 0x01}, one...)))

	clientCodec := &pooledCodec{pool: mem.DefaultBufferPool()}
	require.NoError(t, clientCodec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(marshalDownloadShard(t, makeDownloadShard(testMaxRows+1, 0)))}, &types.DownloadShardResponse{}))
}

func FuzzDownloadViewsParity(f *testing.F) {
	seed, err := makeDownloadShard(3, 2).Marshal()
	require.NoError(f, err)
	f.Add(seed)
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0})
	f.Add([]byte{0x0a, 0x80})
	f.Fuzz(func(t *testing.T, wire []byte) {
		if len(wire) > 64<<10 {
			t.Skip()
		}
		var got, want types.DownloadShardResponse
		wantErr := want.Unmarshal(wire)
		var gotErr error
		if !unmarshalDownloadViews(wire, &got) {
			gotErr = got.Unmarshal(wire)
		}
		require.Equal(t, wantErr, gotErr)
		require.Equal(t, want, got)
		codec := &pooledCodec{maxShardRows: 16, maxProofSegments: 16}
		validationErr := codec.validateDownloadShard(wire)
		for _, input := range []mem.BufferSlice{
			{mem.SliceBuffer(wire)},
			{mem.SliceBuffer(wire[:len(wire)/2]), mem.SliceBuffer(wire[len(wire)/2:])},
		} {
			var decoded types.DownloadShardResponse
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
