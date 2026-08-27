package grpc

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/protobuf/encoding/protowire"
)

// This file bounds the repeated-field cardinality of an UploadShardRequest
// before the generated protobuf decoder runs. The decoder allocates a heap
// object per row and a slice entry per proof segment with no upper bound, so a
// compact message (an empty BlobRow is ~2 wire bytes) could otherwise amplify
// into decoded memory far beyond its wire size and exhaust server memory. The
// pre-scan allocates nothing: it counts field occurrences and skips values.

// validateUploadShardCardinality rejects UploadShardRequest wire bytes whose
// BlobShard.Rows or BlobRow.Proof cardinality exceeds its protocol bound.
func validateUploadShardCardinality(data []byte) error {
	return scanBytesField(data, uploadShardRequestFieldShard, validateBlobShardCardinality)
}

func validateBlobShardCardinality(shard []byte) error {
	rows := 0
	return scanBytesField(shard, blobShardFieldRows, func(row []byte) error {
		rows++
		if rows > types.MaxBlobRowsPerShard {
			return fmt.Errorf("fibre-proto codec: shard exceeds %d rows", types.MaxBlobRowsPerShard)
		}
		return validateBlobRowCardinality(row)
	})
}

func validateBlobRowCardinality(row []byte) error {
	proofs := 0
	return scanBytesField(row, blobRowFieldProof, func([]byte) error {
		proofs++
		if proofs > types.MaxProofSegmentsPerRow {
			return fmt.Errorf("fibre-proto codec: row exceeds %d proof segments", types.MaxProofSegmentsPerRow)
		}
		return nil
	})
}

// scanBytesField walks a proto message's wire bytes, calling visit with the
// payload of every occurrence of the length-delimited field num and skipping
// all other fields. It returns visit's first error, or a wire error for
// malformed bytes.
func scanBytesField(data []byte, num protowire.Number, visit func(payload []byte) error) error {
	for len(data) > 0 {
		fieldNum, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]

		if fieldNum == num && typ == protowire.BytesType {
			payload, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return protowire.ParseError(n)
			}
			if err := visit(payload); err != nil {
				return err
			}
			data = data[n:]
			continue
		}

		n = protowire.ConsumeFieldValue(fieldNum, typ, data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
	}
	return nil
}
