package grpc

import (
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// Scatter-gather marshalers for UploadShardRequest and DownloadShardResponse.
// Emit row payloads (BlobRow.Data, BlobRow.Proof, BlobShard.Rlcs) as
// zero-copy mem.SliceBuffer views over the caller's existing buffers
// instead of copying them into a single contiguous wire buffer. The
// resulting bytes are bit-identical to gogoproto's MarshalToSizedBuffer.
//
// IMPORTANT: this is a hand-rolled proto encoder for specific message
// shapes. Any new field added to UploadShardRequest, DownloadShardResponse,
// BlobShard, or BlobRow MUST be reflected here, or it will be silently
// dropped on the wire. The fuzz parity tests in codec_scatter_test.go are
// the safety net — keep them green when modifying proto types.

const (
	scatterFramingInitialCap = 8 << 10 // 8 KiB

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

// scatterWriter accumulates small framing bytes and zero-copy payload views.
// Segments index byte ranges within framing (not slices) so framing can grow
// freely; they are sliced into mem.SliceBuffer only in bufferSlice.
type scatterWriter struct {
	framing   []byte
	segs      []scatterSegment
	flushFrom int
}

type scatterSegment struct {
	start, end int
	data       []byte // zero-copy slice to append after framing[start:end]; nil = none
}

func newScatterWriter() *scatterWriter {
	return &scatterWriter{
		framing: make([]byte, 0, scatterFramingInitialCap),
		segs:    make([]scatterSegment, 0, 64),
	}
}

// pushData ends the current framing run and appends data after it zero-copy.
func (w *scatterWriter) pushData(data []byte) {
	w.segs = append(w.segs, scatterSegment{start: w.flushFrom, end: len(w.framing), data: data})
	w.flushFrom = len(w.framing)
}

// appendShard encodes shard as field. Match gogoproto: omit entirely when nil.
func (w *scatterWriter) appendShard(field protowire.Number, shard *types.BlobShard) {
	if shard == nil {
		return
	}
	w.framing = protowire.AppendTag(w.framing, field, protowire.BytesType)
	w.framing = protowire.AppendVarint(w.framing, uint64(blobShardSize(shard)))

	for _, row := range shard.Rows {
		w.framing = protowire.AppendTag(w.framing, blobShardFieldRows, protowire.BytesType)
		w.framing = protowire.AppendVarint(w.framing, uint64(blobRowSize(row)))

		if row == nil {
			continue
		}
		if row.Index != 0 {
			w.framing = protowire.AppendTag(w.framing, blobRowFieldIndex, protowire.VarintType)
			w.framing = protowire.AppendVarint(w.framing, uint64(row.Index))
		}
		if len(row.Data) > 0 {
			w.framing = protowire.AppendTag(w.framing, blobRowFieldData, protowire.BytesType)
			w.framing = protowire.AppendVarint(w.framing, uint64(len(row.Data)))
			w.pushData(row.Data)
		}
		for _, seg := range row.Proof {
			w.framing = protowire.AppendTag(w.framing, blobRowFieldProof, protowire.BytesType)
			w.framing = protowire.AppendVarint(w.framing, uint64(len(seg)))
			w.pushData(seg)
		}
	}

	if len(shard.Rlcs) > 0 {
		w.framing = protowire.AppendTag(w.framing, blobShardFieldRlcs, protowire.BytesType)
		w.framing = protowire.AppendVarint(w.framing, uint64(len(shard.Rlcs)))
		w.pushData(shard.Rlcs)
	}
}

func (w *scatterWriter) bufferSlice() mem.BufferSlice {
	segs := w.segs
	if w.flushFrom != len(w.framing) {
		segs = append(segs, scatterSegment{start: w.flushFrom, end: len(w.framing)})
	}

	bs := make(mem.BufferSlice, 0, 2*len(segs))
	for _, seg := range segs {
		if seg.end > seg.start {
			bs = append(bs, mem.SliceBuffer(w.framing[seg.start:seg.end]))
		}
		if seg.data != nil {
			bs = append(bs, mem.SliceBuffer(seg.data))
		}
	}
	return bs
}

func marshalUploadShardRequestScatter(req *types.UploadShardRequest) (mem.BufferSlice, error) {
	w := newScatterWriter()

	// Field 1: Promise (small; marshal contiguously into framing).
	// Match gogoproto: omit entirely when nil.
	if req.Promise != nil {
		promiseSize := req.Promise.Size()
		w.framing = protowire.AppendTag(w.framing, uploadShardRequestFieldPromise, protowire.BytesType)
		w.framing = protowire.AppendVarint(w.framing, uint64(promiseSize))
		if promiseSize > 0 {
			base := len(w.framing)
			w.framing = append(w.framing, make([]byte, promiseSize)...)
			if _, err := req.Promise.MarshalToSizedBuffer(w.framing[base : base+promiseSize]); err != nil {
				return nil, err
			}
		}
	}

	// Field 2: Shard envelope.
	w.appendShard(uploadShardRequestFieldShard, req.Shard)
	return w.bufferSlice(), nil
}

// marshalDownloadShardResponseScatter keeps the server from copying a whole
// shard into a second contiguous buffer for every download response.
func marshalDownloadShardResponseScatter(resp *types.DownloadShardResponse) mem.BufferSlice {
	w := newScatterWriter()
	w.appendShard(downloadShardResponseFieldShard, resp.Shard)
	return w.bufferSlice()
}
