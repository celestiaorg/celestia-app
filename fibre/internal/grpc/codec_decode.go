package grpc

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/protobuf/encoding/protowire"
)

// validateUploadShard rejects requests with too many rows or proof segments
// before the full decoder allocates memory for them. It counts rows without
// allocating per row, checks the row limit, then counts proofs for those rows.
func (c *pooledCodec) validateUploadShard(data []byte) error {
	var rowCount types.RowCountUploadShard
	if err := rowCount.Unmarshal(data); err != nil {
		return fmt.Errorf("fibre-proto codec: %w", err)
	}
	if rowCount.Shard != nil && len(rowCount.Shard.Rows) > c.maxShardRows {
		return fmt.Errorf("fibre-proto codec: shard exceeds %d rows", c.maxShardRows)
	}

	var proofCount types.ProofCountUploadShard
	if err := proofCount.Unmarshal(data); err != nil {
		return fmt.Errorf("fibre-proto codec: %w", err)
	}
	if proofCount.Shard == nil {
		return nil
	}
	for i := range proofCount.Shard.Rows {
		if len(proofCount.Shard.Rows[i].Proof) > c.maxProofSegments {
			return fmt.Errorf("fibre-proto codec: row exceeds %d proof segments", c.maxProofSegments)
		}
	}
	return nil
}

// validateDownloadShard mirrors validateUploadShard for download responses,
// whose shard is field 1 of the top-level message. Repeated shard fields merge
// on decode, so their row counts add up. Parse failures are left to the decoder.
func (c *pooledCodec) validateDownloadShard(data []byte) error {
	rows := 0
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil
		}
		data = data[n:]
		if num != 1 || typ != protowire.BytesType {
			if n = protowire.ConsumeFieldValue(num, typ, data); n < 0 {
				return nil
			}
			data = data[n:]
			continue
		}
		shard, n := protowire.ConsumeBytes(data)
		if n < 0 {
			return nil
		}
		data = data[n:]

		var rowCount types.RowCountBlobShard
		if err := rowCount.Unmarshal(shard); err != nil {
			return fmt.Errorf("fibre-proto codec: %w", err)
		}
		rows += len(rowCount.Rows)
		if rows > c.maxShardRows {
			return fmt.Errorf("fibre-proto codec: shard exceeds %d rows", c.maxShardRows)
		}

		var proofCount types.ProofCountBlobShard
		if err := proofCount.Unmarshal(shard); err != nil {
			return fmt.Errorf("fibre-proto codec: %w", err)
		}
		for i := range proofCount.Rows {
			if len(proofCount.Rows[i].Proof) > c.maxProofSegments {
				return fmt.Errorf("fibre-proto codec: row exceeds %d proof segments", c.maxProofSegments)
			}
		}
	}
	return nil
}
