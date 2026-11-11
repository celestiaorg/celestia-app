package grpc

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// Scatter-gather marshaler for UploadShardRequest. Emits BlobRow.Data and
// BlobShard.Rlcs as zero-copy mem.SliceBuffer views over the caller's
// existing buffers instead of copying them into a single contiguous wire
// buffer. Small fields, including proof segments, are copied into the
// framing buffer, since a view per 32-byte segment costs more than the copy.
// The resulting bytes are bit-identical to gogoproto's MarshalToSizedBuffer.
//
// IMPORTANT: this is a hand-rolled proto encoder for one specific message
// shape. Any new field added to UploadShardRequest, BlobShard, or BlobRow
// MUST be reflected here, or it will be silently dropped on the wire. The
// fuzz parity test in codec_scatter_test.go is the safety net — keep it
// green when modifying proto types.

const (
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

// scatterFramingSize returns the bytes of req that are copied into framing:
// everything but row data and RLCs.
func scatterFramingSize(req *types.UploadShardRequest) int {
	size := 0
	if req.Promise != nil {
		size += protowire.SizeTag(uploadShardRequestFieldPromise) + protowire.SizeBytes(req.Promise.Size())
	}
	if req.Shard != nil {
		size += protowire.SizeTag(uploadShardRequestFieldShard) + protowire.SizeBytes(blobShardSize(req.Shard))
		size -= len(req.Shard.Rlcs)
		for _, row := range req.Shard.Rows {
			if row != nil {
				size -= len(row.Data)
			}
		}
	}
	return size
}

// marshalUploadShardRequestScatter takes framing from pool when it is large
// enough to pool and hands it to gRPC as one ref-counted buffer that returns
// to pool after the write.
//
// With a nil owner every other view is a plain mem.SliceBuffer. Otherwise each
// row data view holds a reference on owner until gRPC frees it; rows below the
// pooling threshold are copied instead, since gRPC would not ref-count them.
// RLC views are GC-owned either way.
func marshalUploadShardRequestScatter(req *types.UploadShardRequest, pool mem.BufferPool, owner *rowOwner) (mem.BufferSlice, error) {
	f := newScatterFraming(pool, scatterFramingSize(req))
	if req.Shard != nil {
		f.segs = make([]scatterSegment, 0, len(req.Shard.Rows)+2)
	}
	if err := f.appendPromise(req.Promise); err != nil {
		f.discard()
		return nil, err
	}
	f.appendShard(req.Shard)
	f.finish()
	return f.views(owner)
}

// scatterFraming accumulates the contiguous wire bytes of a request and
// records where zero-copy views are spliced between them.
type scatterFraming struct {
	buf       []byte
	segs      []scatterSegment
	flushFrom int

	// pooled is buf's backing array when it came from pool; nil when GC-owned.
	pool   mem.BufferPool
	pooled *[]byte
}

// scatterSegment indexes a byte range within the framing buffer (not a slice)
// so the buffer can grow freely; it is sliced into views only after all writes.
type scatterSegment struct {
	start, end int
	data       []byte // zero-copy slice to append after buf[start:end]; nil = none
	row        bool   // data is row data
}

func newScatterFraming(pool mem.BufferPool, size int) scatterFraming {
	if mem.IsBelowBufferPoolingThreshold(size) {
		return scatterFraming{buf: make([]byte, 0, size)}
	}
	pooled := pool.Get(size)
	return scatterFraming{buf: (*pooled)[:0], pool: pool, pooled: pooled}
}

// discard returns pooled framing that will not be handed to gRPC.
func (f *scatterFraming) discard() {
	if f.pooled != nil {
		f.pool.Put(f.pooled)
	}
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

// appendShard writes the field 2 envelope. Match gogoproto: omit entirely
// when nil.
func (f *scatterFraming) appendShard(shard *types.BlobShard) {
	if shard == nil {
		return
	}
	f.buf = protowire.AppendTag(f.buf, uploadShardRequestFieldShard, protowire.BytesType)
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
	addFraming := func(start, end int) { add(f.buf[start:end]) }
	if f.pooled != nil {
		*f.pooled = f.buf
		root := mem.NewBuffer(f.pooled, f.pool)
		defer root.Free() // each framing slice holds its own reference
		addFraming = func(start, end int) { bs = append(bs, root.Slice(start, end)) }
	}
	for _, seg := range f.segs {
		if seg.end > seg.start {
			addFraming(seg.start, seg.end)
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
