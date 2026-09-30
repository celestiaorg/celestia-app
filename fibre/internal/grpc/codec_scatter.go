package grpc

import (
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// Scatter-gather marshalers for UploadShardRequest and DownloadShardResponse.
// They emit BlobRow.Data and BlobShard.Rlcs as zero-copy mem.SliceBuffer
// views over the caller's existing buffers instead of copying them into a
// single contiguous wire buffer. Small fields, including proof segments, are
// copied into the framing buffer, since a view per 32-byte segment costs more
// than the copy. The resulting bytes are bit-identical to gogoproto's
// MarshalToSizedBuffer.
//
// IMPORTANT: these are hand-rolled proto encoders for specific message
// shapes. Any new field added to UploadShardRequest, DownloadShardResponse,
// BlobShard, or BlobRow MUST be reflected here, or it will be silently dropped
// on the wire. The fuzz parity test in codec_scatter_test.go is the safety
// net — keep it green when modifying proto types.

const (
	uploadShardRequestFieldPromise = 1
	uploadShardRequestFieldShard   = 2

	downloadShardResponseFieldShard = 1

	blobShardFieldRows = 1
	blobShardFieldRlcs = 2

	blobRowFieldIndex = 1
	blobRowFieldData  = 2
	blobRowFieldProof = 3
)

func blobRowSize(row *types.BlobRow) int {
	if row == nil {
		return 0
	}
	size := 0
	if row.Index != 0 {
		size += protowire.SizeTag(blobRowFieldIndex) + protowire.SizeVarint(uint64(row.Index))
	}
	if len(row.Data) > 0 {
		size += protowire.SizeTag(blobRowFieldData) + protowire.SizeBytes(len(row.Data))
	}
	for _, seg := range row.Proof {
		size += protowire.SizeTag(blobRowFieldProof) + protowire.SizeBytes(len(seg))
	}
	return size
}

func blobShardSize(shard *types.BlobShard) int {
	if shard == nil {
		return 0
	}
	size := 0
	for _, row := range shard.Rows {
		rowLen := blobRowSize(row)
		size += protowire.SizeTag(blobShardFieldRows) + protowire.SizeBytes(rowLen)
	}
	if len(shard.Rlcs) > 0 {
		size += protowire.SizeTag(blobShardFieldRlcs) + protowire.SizeBytes(len(shard.Rlcs))
	}
	return size
}

// shardFramingSize returns the bytes of a length-delimited shard field that
// are copied into framing: everything but row data and RLCs.
func shardFramingSize(num protowire.Number, shard *types.BlobShard) int {
	if shard == nil {
		return 0
	}
	size := protowire.SizeTag(num) + protowire.SizeBytes(blobShardSize(shard)) - len(shard.Rlcs)
	for _, row := range shard.Rows {
		if row != nil {
			size -= len(row.Data)
		}
	}
	return size
}

// scatterBuffer collects framing bytes and the views spliced between them.
type scatterBuffer struct {
	framing   []byte
	segs      []scatterSegment
	flushFrom int
}

// scatterSegment indexes a byte range within framing (not a slice) so framing
// can grow freely, followed by an optional zero-copy view.
type scatterSegment struct {
	start, end int
	data       []byte // zero-copy slice to append after framing[start:end]; nil = none
}

func (b *scatterBuffer) view(data []byte) {
	b.segs = append(b.segs, scatterSegment{start: b.flushFrom, end: len(b.framing), data: data})
	b.flushFrom = len(b.framing)
}

// appendShard writes shard as length-delimited field num. Match gogoproto:
// callers omit a nil shard entirely.
func (b *scatterBuffer) appendShard(num protowire.Number, shard *types.BlobShard) {
	b.framing = protowire.AppendTag(b.framing, num, protowire.BytesType)
	b.framing = protowire.AppendVarint(b.framing, uint64(blobShardSize(shard)))

	for _, row := range shard.Rows {
		rowLen := blobRowSize(row)
		b.framing = protowire.AppendTag(b.framing, blobShardFieldRows, protowire.BytesType)
		b.framing = protowire.AppendVarint(b.framing, uint64(rowLen))

		if row == nil {
			continue
		}
		if row.Index != 0 {
			b.framing = protowire.AppendTag(b.framing, blobRowFieldIndex, protowire.VarintType)
			b.framing = protowire.AppendVarint(b.framing, uint64(row.Index))
		}
		if len(row.Data) > 0 {
			b.framing = protowire.AppendTag(b.framing, blobRowFieldData, protowire.BytesType)
			b.framing = protowire.AppendVarint(b.framing, uint64(len(row.Data)))
			b.view(row.Data)
		}
		for _, seg := range row.Proof {
			b.framing = protowire.AppendTag(b.framing, blobRowFieldProof, protowire.BytesType)
			b.framing = protowire.AppendBytes(b.framing, seg)
		}
	}

	if len(shard.Rlcs) > 0 {
		b.framing = protowire.AppendTag(b.framing, blobShardFieldRlcs, protowire.BytesType)
		b.framing = protowire.AppendVarint(b.framing, uint64(len(shard.Rlcs)))
		b.view(shard.Rlcs)
	}
}

// slice returns the framing and views in wire order. Framing is sliced only
// now, after all writes.
func (b *scatterBuffer) slice() mem.BufferSlice {
	if b.flushFrom != len(b.framing) {
		b.segs = append(b.segs, scatterSegment{start: b.flushFrom, end: len(b.framing)})
	}
	bs := make(mem.BufferSlice, 0, 2*len(b.segs))
	for _, seg := range b.segs {
		if seg.end > seg.start {
			bs = append(bs, mem.SliceBuffer(b.framing[seg.start:seg.end]))
		}
		if seg.data != nil {
			bs = append(bs, mem.SliceBuffer(seg.data))
		}
	}
	return bs
}

func marshalUploadShardRequestScatter(req *types.UploadShardRequest) (mem.BufferSlice, error) {
	framingSize := shardFramingSize(uploadShardRequestFieldShard, req.Shard)
	if req.Promise != nil {
		framingSize += protowire.SizeTag(uploadShardRequestFieldPromise) + protowire.SizeBytes(req.Promise.Size())
	}
	b := scatterBuffer{framing: make([]byte, 0, framingSize)}
	if req.Shard != nil {
		b.segs = make([]scatterSegment, 0, len(req.Shard.Rows)+2)
	}

	// Field 1: Promise (small; marshal contiguously into framing).
	// Match gogoproto: omit entirely when nil.
	if req.Promise != nil {
		promiseSize := req.Promise.Size()
		b.framing = protowire.AppendTag(b.framing, uploadShardRequestFieldPromise, protowire.BytesType)
		b.framing = protowire.AppendVarint(b.framing, uint64(promiseSize))
		if promiseSize > 0 {
			base := len(b.framing)
			b.framing = append(b.framing, make([]byte, promiseSize)...)
			if _, err := req.Promise.MarshalToSizedBuffer(b.framing[base : base+promiseSize]); err != nil {
				return nil, err
			}
		}
	}

	// Field 2: Shard envelope.
	if req.Shard != nil {
		b.appendShard(uploadShardRequestFieldShard, req.Shard)
	}
	return b.slice(), nil
}

func marshalDownloadShardResponseScatter(resp *types.DownloadShardResponse) mem.BufferSlice {
	if resp.Shard == nil {
		return mem.BufferSlice{}
	}
	b := scatterBuffer{
		framing: make([]byte, 0, shardFramingSize(downloadShardResponseFieldShard, resp.Shard)),
		segs:    make([]scatterSegment, 0, len(resp.Shard.Rows)+2),
	}
	b.appendShard(downloadShardResponseFieldShard, resp.Shard)
	return b.slice()
}
