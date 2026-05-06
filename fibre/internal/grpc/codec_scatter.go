package grpc

import (
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// Scatter-gather marshaler for [types.UploadShardRequest].
//
// The default proto.Marshal — and our pooledCodec MarshalToSizedBuffer
// fallback — both build a single contiguous wire buffer by walking the
// message tree and copying every field's bytes into it. For a 28 MiB
// BlobShard that is ~28 MiB of memmove on every send, multiplied by 9
// fan-out targets and 20 in-flight blobs at c=20. The Pyroscope profile
// attributed ~21% of encoder CPU to runtime.memmove even after we shipped
// buffer pooling.
//
// This codec emits the protobuf wire format as a [mem.BufferSlice] in
// which row payload bytes (BlobRow.Data and BlobRow.Proof segments) appear
// as their own [mem.SliceBuffer] entries that reference the existing
// slab-pool buffers — zero copy. gRPC's HTTP/2 transport writes each
// Buffer to the wire in order; a 1 MiB bufio.Writer at the transport
// layer combines the small framing chunks with the bulk row data into
// normal-sized TCP segments, so we don't pay extra syscalls for the
// fragmentation.
//
// The framing buffer (proto tags + length prefixes + the small Promise
// field, ~8 KiB total even for a fully-loaded shard) is allocated fresh
// per Marshal and reclaimed by the GC. That is a negligible cost
// (~240 KiB/sec at 30 Marshal/sec) compared to the 28 MiB memmove it
// replaces.
//
// Wire format produced is bit-identical to gogoproto's MarshalToSizedBuffer
// output: same field numbers, same wire types, same length prefixes. The
// receiving side decodes with the standard Unmarshal — no protocol change.

const (
	uploadShardRequestFieldPromise = 1
	uploadShardRequestFieldShard   = 2

	blobShardFieldRows         = 1
	blobShardFieldCoefficients = 2
	blobShardFieldRoot         = 3

	blobRowFieldIndex = 1
	blobRowFieldData  = 2
	blobRowFieldProof = 3
)

// blobRowSize returns the encoded wire size of a [types.BlobRow].
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

// blobShardSize returns the encoded wire size of a [types.BlobShard].
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

// marshalUploadShardRequestScatter emits an UploadShardRequest as a
// [mem.BufferSlice] without copying the row payload bytes. The slice
// alternates short framing chunks (proto tags + length prefixes + the
// small Promise field) with [mem.SliceBuffer] views of the caller's
// existing row data — gRPC writes them sequentially to the wire, and the
// receiving side reassembles a standard protobuf message stream.
func marshalUploadShardRequestScatter(req *types.UploadShardRequest) (mem.BufferSlice, error) {
	// 8 KiB up front fits the framing for a fully-loaded shard
	// (~256 rows × ~30 bytes of framing each + outer envelope).
	framing := make([]byte, 0, 8<<10)

	// Track byte ranges within `framing` to be emitted as their own
	// SliceBuffer, optionally followed by a zero-copy data slice.
	type segment struct {
		start, end int
		data       []byte // zero-copy slice to insert AFTER framing[start:end]; nil = none
	}
	segs := make([]segment, 0, 64)
	flushFrom := 0
	pushFraming := func(end int, data []byte) {
		segs = append(segs, segment{start: flushFrom, end: end, data: data})
		flushFrom = end
	}

	// === Field 1: Promise (small, marshal contiguously into framing) ===
	promiseSize := req.Promise.Size()
	framing = protowire.AppendTag(framing, uploadShardRequestFieldPromise, protowire.BytesType)
	framing = protowire.AppendVarint(framing, uint64(promiseSize))
	if promiseSize > 0 {
		// gogoproto's MarshalToSizedBuffer writes from the END of the
		// reserved region toward the beginning and returns the actual
		// number of bytes written (n <= size). Reserve, write, then
		// shift left if n < size.
		base := len(framing)
		framing = append(framing, make([]byte, promiseSize)...)
		n, err := req.Promise.MarshalToSizedBuffer(framing[base : base+promiseSize])
		if err != nil {
			return nil, err
		}
		if n != promiseSize {
			copy(framing[base:], framing[base+promiseSize-n:base+promiseSize])
			framing = framing[:base+n]
		}
	}

	// === Field 2: Shard envelope tag + varint(shardSize) ===
	shardSize := blobShardSize(req.Shard)
	framing = protowire.AppendTag(framing, uploadShardRequestFieldShard, protowire.BytesType)
	framing = protowire.AppendVarint(framing, uint64(shardSize))

	// === Per-row encoding ===
	for _, row := range req.Shard.Rows {
		rowLen := blobRowSize(row)
		framing = protowire.AppendTag(framing, blobShardFieldRows, protowire.BytesType)
		framing = protowire.AppendVarint(framing, uint64(rowLen))

		if row.Index != 0 {
			framing = protowire.AppendTag(framing, blobRowFieldIndex, protowire.VarintType)
			framing = protowire.AppendVarint(framing, uint64(row.Index))
		}
		if len(row.Data) > 0 {
			framing = protowire.AppendTag(framing, blobRowFieldData, protowire.BytesType)
			framing = protowire.AppendVarint(framing, uint64(len(row.Data)))
			pushFraming(len(framing), row.Data) // ZERO-COPY
		}
		for _, seg := range row.Proof {
			framing = protowire.AppendTag(framing, blobRowFieldProof, protowire.BytesType)
			framing = protowire.AppendVarint(framing, uint64(len(seg)))
			pushFraming(len(framing), seg) // ZERO-COPY
		}
	}

	// === BlobShard.Coefficients + Root (zero-copy) ===
	if len(req.Shard.Coefficients) > 0 {
		framing = protowire.AppendTag(framing, blobShardFieldCoefficients, protowire.BytesType)
		framing = protowire.AppendVarint(framing, uint64(len(req.Shard.Coefficients)))
		pushFraming(len(framing), req.Shard.Coefficients)
	}
	if len(req.Shard.Root) > 0 {
		framing = protowire.AppendTag(framing, blobShardFieldRoot, protowire.BytesType)
		framing = protowire.AppendVarint(framing, uint64(len(req.Shard.Root)))
		pushFraming(len(framing), req.Shard.Root)
	}

	// Flush trailing framing (handles the empty-shard case and anything
	// emitted after the last data slice).
	if flushFrom != len(framing) {
		segs = append(segs, segment{start: flushFrom, end: len(framing)})
	}

	// === Assemble BufferSlice ===
	//
	// Each segment becomes a SliceBuffer view of framing[start:end],
	// optionally followed by a SliceBuffer of the zero-copy data slice.
	// `framing` is referenced by all the framing-segment SliceBuffers; it
	// stays alive until the GC reclaims it after gRPC has written and
	// freed the BufferSlice.
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
