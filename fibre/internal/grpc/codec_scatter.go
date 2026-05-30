package grpc

import (
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// Scatter-gather marshalers for the two large fibre messages that wrap a
// BlobShard: UploadShardRequest (send side) and DownloadShardResponse (serve
// side). Emits row payloads (BlobRow.Data, BlobRow.Proof,
// BlobShard.Coefficients, BlobShard.Root) as zero-copy mem.SliceBuffer views
// over the caller's existing buffers instead of copying them into a single
// contiguous wire buffer. The resulting bytes are bit-identical to gogoproto's
// MarshalToSizedBuffer.
//
// IMPORTANT: this is a hand-rolled proto encoder for specific message shapes.
// Any new field added to UploadShardRequest, DownloadShardResponse, BlobShard,
// or BlobRow MUST be reflected here, or it will be silently dropped on the
// wire. The fuzz parity tests in codec_scatter_test.go are the safety net —
// keep them green when modifying proto types.

const (
	uploadShardRequestFieldPromise = 1
	uploadShardRequestFieldShard   = 2

	downloadShardResponseFieldShard = 1

	blobShardFieldRows         = 1
	blobShardFieldCoefficients = 2
	blobShardFieldRoot         = 3

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
	if len(shard.Coefficients) > 0 {
		size += protowire.SizeTag(blobShardFieldCoefficients) + protowire.SizeBytes(len(shard.Coefficients))
	}
	if len(shard.Root) > 0 {
		size += protowire.SizeTag(blobShardFieldRoot) + protowire.SizeBytes(len(shard.Root))
	}
	return size
}

// scatterSegment indexes a byte range within a scatterBuilder's framing buffer
// (not a slice, so framing can grow freely) plus an optional zero-copy payload
// to emit right after it.
type scatterSegment struct {
	start, end int
	data       []byte // zero-copy slice appended after framing[start:end]; nil = none
}

// scatterBuilder accumulates protobuf framing bytes interleaved with zero-copy
// references to caller-owned payloads, assembling a mem.BufferSlice in finish.
type scatterBuilder struct {
	framing   []byte
	segs      []scatterSegment
	flushFrom int
}

func newScatterBuilder() *scatterBuilder {
	return &scatterBuilder{
		framing: make([]byte, 0, 8<<10),
		segs:    make([]scatterSegment, 0, 64),
	}
}

// pushPayload closes the framing run written since the last push and records a
// zero-copy reference to data. Call right after appending data's tag+length.
func (b *scatterBuilder) pushPayload(data []byte) {
	b.segs = append(b.segs, scatterSegment{start: b.flushFrom, end: len(b.framing), data: data})
	b.flushFrom = len(b.framing)
}

// finish flushes the trailing framing run and assembles the BufferSlice,
// alternating owned framing slices with zero-copy payload views.
func (b *scatterBuilder) finish() mem.BufferSlice {
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

// appendBlobShard emits shard's fields (rows, coefficients, root). The large
// payloads — row data and the RLC coefficients blob — are referenced zero-copy;
// the small fields (proof segments, root) are copied inline into framing, since
// emitting a separate buffer per 32-byte segment would cost one interface box
// per segment (tens of thousands for a full shard) and bloat gRPC's writev with
// no bandwidth saving. The caller must already have written shard's enclosing
// field tag+length. Shared by upload (field 2) and download (field 1).
func appendBlobShard(b *scatterBuilder, shard *types.BlobShard) {
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
			b.pushPayload(row.Data) // large: zero-copy
		}
		for _, seg := range row.Proof {
			b.framing = protowire.AppendTag(b.framing, blobRowFieldProof, protowire.BytesType)
			b.framing = protowire.AppendVarint(b.framing, uint64(len(seg)))
			b.framing = append(b.framing, seg...) // small: copy inline
		}
	}

	if len(shard.Coefficients) > 0 {
		b.framing = protowire.AppendTag(b.framing, blobShardFieldCoefficients, protowire.BytesType)
		b.framing = protowire.AppendVarint(b.framing, uint64(len(shard.Coefficients)))
		b.pushPayload(shard.Coefficients) // large: zero-copy
	}
	if len(shard.Root) > 0 {
		b.framing = protowire.AppendTag(b.framing, blobShardFieldRoot, protowire.BytesType)
		b.framing = protowire.AppendVarint(b.framing, uint64(len(shard.Root)))
		b.framing = append(b.framing, shard.Root...) // small: copy inline
	}
}

func marshalUploadShardRequestScatter(req *types.UploadShardRequest) (mem.BufferSlice, error) {
	b := newScatterBuilder()

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

	// Field 2: Shard envelope. Match gogoproto: omit entirely when nil.
	if req.Shard != nil {
		b.framing = protowire.AppendTag(b.framing, uploadShardRequestFieldShard, protowire.BytesType)
		b.framing = protowire.AppendVarint(b.framing, uint64(blobShardSize(req.Shard)))
		appendBlobShard(b, req.Shard)
	}

	return b.finish(), nil
}

// marshalDownloadShardResponseScatter is the serve-side counterpart: emits a
// DownloadShardResponse with its BlobShard row payloads referenced zero-copy
// from the server's buffers (which live until gRPC finishes sending). The
// referenced buffers must not be mutated or recycled during the send.
func marshalDownloadShardResponseScatter(resp *types.DownloadShardResponse) (mem.BufferSlice, error) {
	b := newScatterBuilder()

	// Field 1: Shard envelope. Match gogoproto: omit entirely when nil.
	if resp.Shard != nil {
		b.framing = protowire.AppendTag(b.framing, downloadShardResponseFieldShard, protowire.BytesType)
		b.framing = protowire.AppendVarint(b.framing, uint64(blobShardSize(resp.Shard)))
		appendBlobShard(b, resp.Shard)
	}

	return b.finish(), nil
}
