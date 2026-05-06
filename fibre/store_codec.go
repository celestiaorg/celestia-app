package fibre

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/cockroachdb/pebble/v2/vfs"
)

// On-disk shard format (custom binary, all big-endian):
//
//	uint32  version (=1)
//	uint32  root_len; []byte root
//	uint32  coefficients_len; []byte coefficients
//	uint32  num_rows
//	  for each row:
//	    uint32 index
//	    uint32 data_len; []byte data
//	    uint32 num_proof_segments
//	      for each: uint32 segment_len; []byte segment
//
// Replaces gogoproto.Marshal of BlobShard to avoid one 28 MiB allocation +
// memcpy per Put — the proto path dominated the encoder CPU profile
// (memmove + memclr ≈ 39%) at high concurrency.
const shardCodecVersion uint32 = 1

// writeShardBinary serializes shard to w. Row payloads are written from
// their existing buffers without an intermediate user-space copy.
func writeShardBinary(w io.Writer, shard *types.BlobShard) error {
	// Stack-allocated scratch for length prefixes. 64 bytes covers the
	// largest single batched header (one row's index + lengths).
	var stack [64]byte
	buf := stack[:0]

	buf = binary.BigEndian.AppendUint32(buf, shardCodecVersion)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(shard.Root)))
	if _, err := w.Write(buf); err != nil {
		return err
	}
	if _, err := w.Write(shard.Root); err != nil {
		return err
	}

	buf = buf[:0]
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(shard.Coefficients)))
	if _, err := w.Write(buf); err != nil {
		return err
	}
	if _, err := w.Write(shard.Coefficients); err != nil {
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

// Caps used to reject corrupt files cheaply, before allocating.
const (
	shardLengthLimit    = 1 << 30 // any single byte-length prefix
	maxShardRows        = 1 << 16 // 4× TotalRows at current protocol params
	maxRowProofSegments = 64      // covers 2^64-leaf trees
)

func readUint32(r io.Reader, scratch []byte) (uint32, error) {
	if _, err := io.ReadFull(r, scratch[:4]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(scratch[:4]), nil
}

func readBytes(r io.Reader, n uint32) ([]byte, error) {
	if n > shardLengthLimit {
		return nil, fmt.Errorf("length %d exceeds shard limit %d", n, shardLengthLimit)
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]byte, n)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, err
	}
	return out, nil
}

func readShardBinary(r io.Reader) (*types.BlobShard, error) {
	var scratch [4]byte
	version, err := readUint32(r, scratch[:])
	if err != nil {
		return nil, fmt.Errorf("reading version: %w", err)
	}
	if version != shardCodecVersion {
		return nil, fmt.Errorf("unsupported shard codec version %d (want %d)", version, shardCodecVersion)
	}

	rootLen, err := readUint32(r, scratch[:])
	if err != nil {
		return nil, fmt.Errorf("reading root len: %w", err)
	}
	root, err := readBytes(r, rootLen)
	if err != nil {
		return nil, fmt.Errorf("reading root: %w", err)
	}

	coeffsLen, err := readUint32(r, scratch[:])
	if err != nil {
		return nil, fmt.Errorf("reading coefficients len: %w", err)
	}
	coeffs, err := readBytes(r, coeffsLen)
	if err != nil {
		return nil, fmt.Errorf("reading coefficients: %w", err)
	}

	numRows, err := readUint32(r, scratch[:])
	if err != nil {
		return nil, fmt.Errorf("reading num rows: %w", err)
	}
	if numRows > maxShardRows {
		return nil, fmt.Errorf("num rows %d exceeds limit %d", numRows, maxShardRows)
	}

	shard := &types.BlobShard{
		Rows:         make([]*types.BlobRow, numRows),
		Coefficients: coeffs,
		Root:         root,
	}
	for i := range numRows {
		index, err := readUint32(r, scratch[:])
		if err != nil {
			return nil, fmt.Errorf("reading row %d index: %w", i, err)
		}
		dataLen, err := readUint32(r, scratch[:])
		if err != nil {
			return nil, fmt.Errorf("reading row %d data len: %w", i, err)
		}
		data, err := readBytes(r, dataLen)
		if err != nil {
			return nil, fmt.Errorf("reading row %d data: %w", i, err)
		}
		numProof, err := readUint32(r, scratch[:])
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
				segLen, err := readUint32(r, scratch[:])
				if err != nil {
					return nil, fmt.Errorf("reading row %d proof %d len: %w", i, j, err)
				}
				seg, err := readBytes(r, segLen)
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

// readShardFile returns [ErrStoreNotFound] when the file is missing.
func readShardFile(filesystem vfs.FS, path string) (*types.BlobShard, error) {
	f, err := filesystem.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrStoreNotFound
		}
		return nil, err
	}
	defer f.Close()
	// Buffered so the many 4-byte length-prefix reads don't each become a
	// syscall; bufio bypasses the buffer for large reads.
	return readShardBinary(bufio.NewReaderSize(f, 1<<20))
}
