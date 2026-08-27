package fibre

import (
	"math/bits"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
)

// TestDecodeLimitsMatchParams guards the hardcoded codec cardinality bounds in
// x/fibre/types against drift from the protocol parameters they are derived from.
func TestDecodeLimitsMatchParams(t *testing.T) {
	p := DefaultProtocolParams

	require.Equal(t, p.MaxRowsPerValidator(), types.MaxBlobRowsPerShard,
		"MaxBlobRowsPerShard must equal MaxRowsPerValidator")

	treeDepth := bits.Len(uint(p.TotalRows() - 1))
	require.Equal(t, treeDepth, types.MaxProofSegmentsPerRow,
		"MaxProofSegmentsPerRow must equal the Merkle tree depth ceil(log2(TotalRows))")
}
