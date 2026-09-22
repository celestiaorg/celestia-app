package grpc

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
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
