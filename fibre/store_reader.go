package fibre

import (
	"encoding/binary"
	"errors"
	"io"
	"math"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
)

// shardReader reads the binary shard format without copying its payload slices.
// The caller must keep the shard unchanged until reading and retries finish.
type shardReader struct {
	parts  [][]byte
	size   int64
	pos    int64
	index  int
	offset int
}

var _ io.ReadSeeker = (*shardReader)(nil)

func newShardReader(shard *types.BlobShard) (*shardReader, error) {
	if shard == nil {
		return nil, errors.New("nil shard")
	}
	proofSegments := 0
	for _, row := range shard.Rows {
		if row == nil {
			return nil, errors.New("nil row in shard")
		}
		proofSegments += len(row.Proof)
	}

	const (
		shardHeaderBytes   = 12 // Version, RLC length, row count.
		rowHeaderBytes     = 12 // Index, data length, proof count.
		segmentHeaderBytes = 4  // Segment length.

		shardParts   = 3 // Header, RLC payload, row count.
		rowParts     = 3 // Header, data payload, proof count.
		segmentParts = 2 // Length header, payload.
	)
	headers := make([]byte, 0, shardHeaderBytes+rowHeaderBytes*len(shard.Rows)+segmentHeaderBytes*proofSegments)
	r := &shardReader{parts: make([][]byte, 0, shardParts+rowParts*len(shard.Rows)+segmentParts*proofSegments)}
	addHeader := func(values ...uint32) {
		start := len(headers)
		for _, value := range values {
			headers = binary.BigEndian.AppendUint32(headers, value)
		}
		r.parts = append(r.parts, headers[start:])
	}

	addHeader(shardCodecVersion, uint32(len(shard.Rlcs)))
	r.parts = append(r.parts, shard.Rlcs)
	addHeader(uint32(len(shard.Rows)))
	for _, row := range shard.Rows {
		addHeader(row.Index, uint32(len(row.Data)))
		r.parts = append(r.parts, row.Data)
		addHeader(uint32(len(row.Proof)))
		for _, segment := range row.Proof {
			addHeader(uint32(len(segment)))
			r.parts = append(r.parts, segment)
		}
	}
	for _, part := range r.parts {
		r.size += int64(len(part))
	}
	return r, nil
}

func (r *shardReader) Read(p []byte) (int, error) {
	if r.pos >= r.size {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) && r.index < len(r.parts) {
		copied := copy(p[n:], r.parts[r.index][r.offset:])
		n += copied
		r.offset += copied
		if r.offset == len(r.parts[r.index]) {
			r.index++
			r.offset = 0
		}
	}
	r.pos += int64(n)
	return n, nil
}

func (r *shardReader) Seek(offset int64, whence int) (int64, error) {
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = r.pos
	case io.SeekEnd:
		base = r.size
	default:
		return 0, errors.New("invalid seek whence")
	}
	if offset > 0 && base > math.MaxInt64-offset {
		return 0, errors.New("seek offset overflow")
	}
	pos := base + offset
	if pos < 0 {
		return 0, errors.New("negative seek position")
	}

	r.pos = pos
	r.index = 0
	for r.index < len(r.parts) && pos >= int64(len(r.parts[r.index])) {
		pos -= int64(len(r.parts[r.index]))
		r.index++
	}
	r.offset = 0
	if r.index < len(r.parts) {
		r.offset = int(pos)
	}
	return r.pos, nil
}
