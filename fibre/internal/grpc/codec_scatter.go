package grpc

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// Scatter-gather marshaler for UploadShardRequest and DownloadShardResponse.
// Emits BlobRow.Data and BlobShard.Rlcs as zero-copy mem.SliceBuffer views
// over the caller's existing buffers instead of copying them into a single
// contiguous wire buffer. Small fields, including proof segments, are copied
// into the framing buffer, since a view per 32-byte segment costs more than
// the copy. The resulting bytes are bit-identical to gogoproto's
// MarshalToSizedBuffer.
//
// IMPORTANT: this is a hand-rolled proto encoder for a few specific message
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

// scatterFramingSize returns the bytes of req that are copied into framing:
// everything but row data and RLCs.
func scatterFramingSize(req *types.UploadShardRequest) int {
	size := shardFramingSize(uploadShardRequestFieldShard, req.Shard)
	if req.Promise != nil {
		size += protowire.SizeTag(uploadShardRequestFieldPromise) + protowire.SizeBytes(req.Promise.Size())
	}
	return size
}

// marshalUploadShardRequestScatter marshals req. With a nil owner every view
// is a plain mem.SliceBuffer. Otherwise each row data view holds a reference
// on owner until gRPC frees it; rows below the pooling threshold are copied
// instead, since gRPC would not ref-count them. Framing and RLC views are
// GC-owned either way.
func marshalUploadShardRequestScatter(req *types.UploadShardRequest, owner *rowOwner) (mem.BufferSlice, error) {
	f := scatterFraming{buf: make([]byte, 0, scatterFramingSize(req))}
	if req.Shard != nil {
		f.segs = make([]scatterSegment, 0, len(req.Shard.Rows)+2)
	}
	if err := f.appendPromise(req.Promise); err != nil {
		return nil, err
	}
	f.appendShard(uploadShardRequestFieldShard, req.Shard)
	f.finish()
	return f.views(owner)
}

// marshalDownloadShardResponseScatter marshals resp with every view a plain
// mem.SliceBuffer: the server keeps the shard alive itself.
func marshalDownloadShardResponseScatter(resp *types.DownloadShardResponse) mem.BufferSlice {
	if resp.Shard == nil {
		return mem.BufferSlice{}
	}
	f := scatterFraming{
		buf:  make([]byte, 0, shardFramingSize(downloadShardResponseFieldShard, resp.Shard)),
		segs: make([]scatterSegment, 0, len(resp.Shard.Rows)+2),
	}
	f.appendShard(downloadShardResponseFieldShard, resp.Shard)
	f.finish()
	bs, _ := f.views(nil) // cannot fail without an owner
	return bs
}

// scatterFraming accumulates the contiguous wire bytes of a request and
// records where zero-copy views are spliced between them.
type scatterFraming struct {
	buf       []byte
	segs      []scatterSegment
	flushFrom int
}

// scatterSegment indexes a byte range within the framing buffer (not a slice)
// so the buffer can grow freely; it is sliced into views only after all writes.
type scatterSegment struct {
	start, end int
	data       []byte // zero-copy slice to append after buf[start:end]; nil = none
	row        bool   // data is row data
}

// splice ends the current framing segment and queues data after it.
func (f *scatterFraming) splice(data []byte, row bool) {
	f.segs = append(f.segs, scatterSegment{start: f.flushFrom, end: len(f.buf), data: data, row: row})
	f.flushFrom = len(f.buf)
}

func (f *scatterFraming) finish() {
	if f.flushFrom != len(f.buf) {
		f.segs = append(f.segs, scatterSegment{start: f.flushFrom, end: len(f.buf)})
	}
}

// appendPromise writes field 1 contiguously; the promise is small. Match
// gogoproto: omit entirely when nil.
func (f *scatterFraming) appendPromise(promise *types.PaymentPromise) error {
	if promise == nil {
		return nil
	}
	size := promise.Size()
	f.buf = protowire.AppendTag(f.buf, uploadShardRequestFieldPromise, protowire.BytesType)
	f.buf = protowire.AppendVarint(f.buf, uint64(size))
	if size > 0 {
		base := len(f.buf)
		f.buf = append(f.buf, make([]byte, size)...)
		if _, err := promise.MarshalToSizedBuffer(f.buf[base : base+size]); err != nil {
			return err
		}
	}
	return nil
}

// appendShard writes shard as length-delimited field num. Match gogoproto:
// omit entirely when nil.
func (f *scatterFraming) appendShard(num protowire.Number, shard *types.BlobShard) {
	if shard == nil {
		return
	}
	f.buf = protowire.AppendTag(f.buf, num, protowire.BytesType)
	f.buf = protowire.AppendVarint(f.buf, uint64(blobShardSize(shard)))
	for _, row := range shard.Rows {
		f.appendRow(row)
	}
	if len(shard.Rlcs) > 0 {
		f.buf = protowire.AppendTag(f.buf, blobShardFieldRlcs, protowire.BytesType)
		f.buf = protowire.AppendVarint(f.buf, uint64(len(shard.Rlcs)))
		f.splice(shard.Rlcs, false)
	}
}

func (f *scatterFraming) appendRow(row *types.BlobRow) {
	f.buf = protowire.AppendTag(f.buf, blobShardFieldRows, protowire.BytesType)
	f.buf = protowire.AppendVarint(f.buf, uint64(blobRowSize(row)))
	if row == nil {
		return
	}
	if row.Index != 0 {
		f.buf = protowire.AppendTag(f.buf, blobRowFieldIndex, protowire.VarintType)
		f.buf = protowire.AppendVarint(f.buf, uint64(row.Index))
	}
	if len(row.Data) > 0 {
		f.buf = protowire.AppendTag(f.buf, blobRowFieldData, protowire.BytesType)
		f.buf = protowire.AppendVarint(f.buf, uint64(len(row.Data)))
		f.splice(row.Data, true)
	}
	for _, seg := range row.Proof {
		f.buf = protowire.AppendTag(f.buf, blobRowFieldProof, protowire.BytesType)
		f.buf = protowire.AppendBytes(f.buf, seg)
	}
}

// views assembles the wire buffers in order: framing bytes interleaved with
// the spliced row data and RLC views.
func (f *scatterFraming) views(owner *rowOwner) (mem.BufferSlice, error) {
	// Converting a mem.SliceBuffer value to mem.Buffer allocates once per
	// view. Keeping the views in one slice and adding pointers to its
	// elements avoids that. views must never grow past its capacity, or
	// earlier pointers would refer to a stale backing array.
	views := make([]mem.SliceBuffer, 0, 2*len(f.segs))
	bs := make(mem.BufferSlice, 0, 2*len(f.segs))
	add := func(b []byte) {
		views = append(views, b)
		bs = append(bs, &views[len(views)-1])
	}
	for _, seg := range f.segs {
		if seg.end > seg.start {
			add(f.buf[seg.start:seg.end])
		}
		switch {
		case seg.data == nil:
		case owner == nil || !seg.row:
			add(seg.data)
		case mem.IsBelowBufferPoolingThreshold(cap(seg.data)):
			add(bytes.Clone(seg.data))
		default:
			view, ok := owner.wrap(seg.data)
			if !ok {
				bs.Free()
				return nil, errUploadReleased
			}
			bs = append(bs, view)
		}
	}
	return bs, nil
}
