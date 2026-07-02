package grpc

import (
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// Scatter-gather marshaler for UploadShardRequest. Emits row payloads
// (BlobRow.Data, BlobRow.Proof, BlobShard.Rlcs) as
// zero-copy mem.SliceBuffer views over the caller's existing buffers
// instead of copying them into a single contiguous wire buffer. The
// resulting bytes are bit-identical to gogoproto's MarshalToSizedBuffer.
//
// IMPORTANT: this is a hand-rolled proto encoder for one specific message
// shape. Any new field added to UploadShardRequest, BlobShard, or BlobRow
// MUST be reflected here, or it will be silently dropped on the wire. The
// fuzz parity test in codec_scatter_test.go is the safety net — keep it
// green when modifying proto types.

const (
	scatterFramingInitialCap = 8 << 10 // 8 KiB

	uploadShardRequestFieldPromise = 1
	uploadShardRequestFieldShard   = 2

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

func marshalUploadShardRequestScatter(req *types.UploadShardRequest) (mem.BufferSlice, error) {
	framing := make([]byte, 0, scatterFramingInitialCap)

	// Segments index byte ranges within framing (not slices) so framing
	// can grow freely; sliced into mem.SliceBuffer only after all writes.
	type segment struct {
		start, end int
		data       []byte // zero-copy slice to append after framing[start:end]; nil = none
	}
	segs := make([]segment, 0, 64)
	flushFrom := 0
	pushFraming := func(end int, data []byte) {
		segs = append(segs, segment{start: flushFrom, end: end, data: data})
		flushFrom = end
	}

	// Field 1: Promise (small; marshal contiguously into framing).
	// Match gogoproto: omit entirely when nil.
	if req.Promise != nil {
		promiseSize := req.Promise.Size()
		framing = protowire.AppendTag(framing, uploadShardRequestFieldPromise, protowire.BytesType)
		framing = protowire.AppendVarint(framing, uint64(promiseSize))
		if promiseSize > 0 {
			base := len(framing)
			framing = append(framing, make([]byte, promiseSize)...)
			if _, err := req.Promise.MarshalToSizedBuffer(framing[base : base+promiseSize]); err != nil {
				return nil, err
			}
		}
	}

	// Field 2: Shard envelope. Match gogoproto: omit entirely when nil.
	if req.Shard != nil {
		shardSize := blobShardSize(req.Shard)
		framing = protowire.AppendTag(framing, uploadShardRequestFieldShard, protowire.BytesType)
		framing = protowire.AppendVarint(framing, uint64(shardSize))

		for _, row := range req.Shard.Rows {
			rowLen := blobRowSize(row)
			framing = protowire.AppendTag(framing, blobShardFieldRows, protowire.BytesType)
			framing = protowire.AppendVarint(framing, uint64(rowLen))

			if row == nil {
				continue
			}
			if row.Index != 0 {
				framing = protowire.AppendTag(framing, blobRowFieldIndex, protowire.VarintType)
				framing = protowire.AppendVarint(framing, uint64(row.Index))
			}
			if len(row.Data) > 0 {
				framing = protowire.AppendTag(framing, blobRowFieldData, protowire.BytesType)
				framing = protowire.AppendVarint(framing, uint64(len(row.Data)))
				pushFraming(len(framing), row.Data)
			}
			for _, seg := range row.Proof {
				framing = protowire.AppendTag(framing, blobRowFieldProof, protowire.BytesType)
				framing = protowire.AppendVarint(framing, uint64(len(seg)))
				pushFraming(len(framing), seg)
			}
		}

		if len(req.Shard.Rlcs) > 0 {
			framing = protowire.AppendTag(framing, blobShardFieldRlcs, protowire.BytesType)
			framing = protowire.AppendVarint(framing, uint64(len(req.Shard.Rlcs)))
			pushFraming(len(framing), req.Shard.Rlcs)
		}
	}

	if flushFrom != len(framing) {
		segs = append(segs, segment{start: flushFrom, end: len(framing)})
	}

	bs := make(mem.BufferSlice, 0, 2*len(segs))
	for _, seg := range segs {
		if seg.end > seg.start {
			bs = append(bs, mem.SliceBuffer(framing[seg.start:seg.end]))
		}
		if seg.data != nil {
			bs = append(bs, mem.SliceBuffer(seg.data))
		}
	}
	return bs, nil
}
