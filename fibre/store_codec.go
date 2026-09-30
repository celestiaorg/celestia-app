package fibre

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
)

// On-disk shard format (custom binary, all big-endian):
//
//	uint32  version (=1)
//	uint32  rlcs_len; []byte rlcs
//	uint32  num_rows
//	  for each row:
//	    uint32 index
//	    uint32 data_len; []byte data
//	    uint32 num_proof_segments
//	      for each: uint32 segment_len; []byte segment
//
// Replaces gogoproto.Marshal of BlobShard to avoid one 28 MiB allocation +
// memcpy per Put — the proto path dominated the encoder CPU profile
// (memmove + memclr ≈ 39%) at high concurrency. Length-prefixed proto
// streaming (e.g. libp2p protoio) still marshals each message body into a
// buffer before writing, so it doesn't address the bottleneck.
const shardCodecVersion uint32 = 1

// writeShardBinary serializes shard to w. Row payloads are written from
// their existing buffers without an intermediate user-space copy.
func writeShardBinary(w io.Writer, shard *types.BlobShard) error {
	// Stack-allocated scratch for length prefixes. 64 bytes covers the
	// largest single batched header (one row's index + lengths).
	var stack [64]byte
	buf := stack[:0]

	buf = binary.BigEndian.AppendUint32(buf, shardCodecVersion)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(shard.Rlcs)))
	if _, err := w.Write(buf); err != nil {
		return err
	}
	if _, err := w.Write(shard.Rlcs); err != nil {
		return err
	}

	buf = buf[:0]
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(shard.Rows)))
	if _, err := w.Write(buf); err != nil {
		return err
	}

	for _, row := range shard.Rows {
		if row == nil {
			return errors.New("nil row in shard")
		}
		buf = buf[:0]
		buf = binary.BigEndian.AppendUint32(buf, row.Index)
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(row.Data)))
		if _, err := w.Write(buf); err != nil {
			return err
		}
		if _, err := w.Write(row.Data); err != nil {
			return err
		}

		buf = buf[:0]
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(row.Proof)))
		if _, err := w.Write(buf); err != nil {
			return err
		}
		for _, seg := range row.Proof {
			buf = buf[:0]
			buf = binary.BigEndian.AppendUint32(buf, uint32(len(seg)))
			if _, err := w.Write(buf); err != nil {
				return err
			}
			if _, err := w.Write(seg); err != nil {
				return err
			}
		}
	}

	return nil
}

// shardBinarySize returns the exact number of bytes writeShardBinary produces
// for shard, without serializing it.
func shardBinarySize(shard *types.BlobShard) int64 {
	// version + rlcs-len prefix + rlcs + rows-count prefix
	size := int64(4 + 4 + len(shard.Rlcs) + 4)
	for _, row := range shard.Rows {
		// index + data-len prefix + data + proof-count prefix
		size += int64(4 + 4 + len(row.Data) + 4)
		for _, seg := range row.Proof {
			size += int64(4 + len(seg)) // seg-len prefix + seg
		}
	}
	return size
}

// Caps used to reject corrupt files cheaply, before allocating.
const (
	shardLengthLimit    = 1 << 30 // any single byte-length prefix
	maxShardRows        = 1 << 16 // 4× TotalRows at current protocol params
	maxRowProofSegments = 64      // covers 2^64-leaf trees
)

// shardSource yields the length prefixes and byte fields of a shard file.
type shardSource interface {
	uint32() (uint32, error)
	bytes(n uint32) ([]byte, error)
}

// readerSource copies fields out of a stream.
type readerSource struct {
	r       io.Reader
	scratch [4]byte
}

func (s *readerSource) uint32() (uint32, error) {
	if _, err := io.ReadFull(s.r, s.scratch[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(s.scratch[:]), nil
}

func (s *readerSource) bytes(n uint32) ([]byte, error) {
	if n > shardLengthLimit {
		return nil, fmt.Errorf("length %d exceeds shard limit %d", n, shardLengthLimit)
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]byte, n)
	if _, err := io.ReadFull(s.r, out); err != nil {
		return nil, err
	}
	return out, nil
}

// sliceSource returns fields as views of one buffer, which must outlive the
// decoded shard.
type sliceSource struct {
	data []byte
}

func (s *sliceSource) uint32() (uint32, error) {
	if len(s.data) < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint32(s.data)
	s.data = s.data[4:]
	return v, nil
}

func (s *sliceSource) bytes(n uint32) ([]byte, error) {
	if n > shardLengthLimit {
		return nil, fmt.Errorf("length %d exceeds shard limit %d", n, shardLengthLimit)
	}
	if n == 0 {
		return nil, nil
	}
	if uint64(len(s.data)) < uint64(n) {
		return nil, io.ErrUnexpectedEOF
	}
	// Appending to one field must not overwrite the next one.
	out := s.data[:n:n]
	s.data = s.data[n:]
	return out, nil
}

// readShardBinary decodes a shard from r, copying every field.
func readShardBinary(r io.Reader) (*types.BlobShard, error) {
	return decodeShard(&readerSource{r: r})
}

// decodeShardBinary decodes a whole shard file. Rows, proofs and RLCs alias
// data, so it must not be modified while the shard is in use. Trailing bytes
// are rejected.
func decodeShardBinary(data []byte) (*types.BlobShard, error) {
	src := &sliceSource{data: data}
	shard, err := decodeShard(src)
	if err != nil {
		return nil, err
	}
	if len(src.data) != 0 {
		return nil, fmt.Errorf("%d trailing bytes after shard", len(src.data))
	}
	return shard, nil
}

func decodeShard(src shardSource) (*types.BlobShard, error) {
	version, err := src.uint32()
	if err != nil {
		return nil, fmt.Errorf("reading version: %w", err)
	}
	if version != shardCodecVersion {
		return nil, fmt.Errorf("unsupported shard codec version %d (want %d)", version, shardCodecVersion)
	}

	rlcsLen, err := src.uint32()
	if err != nil {
		return nil, fmt.Errorf("reading rlcs len: %w", err)
	}
	rlcs, err := src.bytes(rlcsLen)
	if err != nil {
		return nil, fmt.Errorf("reading rlcs: %w", err)
	}

	numRows, err := src.uint32()
	if err != nil {
		return nil, fmt.Errorf("reading num rows: %w", err)
	}
	if numRows > maxShardRows {
		return nil, fmt.Errorf("num rows %d exceeds limit %d", numRows, maxShardRows)
	}

	shard := &types.BlobShard{
		Rows: make([]*types.BlobRow, numRows),
		Rlcs: rlcs,
	}
	for i := range numRows {
		index, err := src.uint32()
		if err != nil {
			return nil, fmt.Errorf("reading row %d index: %w", i, err)
		}
		dataLen, err := src.uint32()
		if err != nil {
			return nil, fmt.Errorf("reading row %d data len: %w", i, err)
		}
		data, err := src.bytes(dataLen)
		if err != nil {
			return nil, fmt.Errorf("reading row %d data: %w", i, err)
		}
		numProof, err := src.uint32()
		if err != nil {
			return nil, fmt.Errorf("reading row %d num proof: %w", i, err)
		}
		var proof [][]byte
		if numProof > 0 {
			if numProof > maxRowProofSegments {
				return nil, fmt.Errorf("row %d num proof %d exceeds limit %d", i, numProof, maxRowProofSegments)
			}
			proof = make([][]byte, numProof)
			for j := range numProof {
				segLen, err := src.uint32()
				if err != nil {
					return nil, fmt.Errorf("reading row %d proof %d len: %w", i, j, err)
				}
				seg, err := src.bytes(segLen)
				if err != nil {
					return nil, fmt.Errorf("reading row %d proof %d: %w", i, j, err)
				}
				proof[j] = seg
			}
		}
		shard.Rows[i] = &types.BlobRow{
			Index: index,
			Data:  data,
			Proof: proof,
		}
	}

	return shard, nil
}
