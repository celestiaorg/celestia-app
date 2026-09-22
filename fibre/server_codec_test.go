package fibre

import (
	"testing"

	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/mem"
)

// TestServerCodecLimits checks the row and proof limits used by the server.
func TestServerCodecLimits(t *testing.T) {
	p := DefaultProtocolParams
	codec := fibregrpc.NewServerCodec(p.MaxRowsPerValidator(), p.MerkleProofDepth())

	unmarshal := func(rows, proofsPerRow int) error {
		proof := make([][]byte, proofsPerRow)
		for i := range proof {
			proof[i] = make([]byte, 32)
		}
		blobRows := make([]*types.BlobRow, rows)
		for i := range blobRows {
			blobRows[i] = &types.BlobRow{Index: uint32(i), Proof: proof}
		}
		buf, err := (&types.UploadShardRequest{Shard: &types.BlobShard{Rows: blobRows}}).Marshal()
		require.NoError(t, err)
		return codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(buf)}, &types.UploadShardRequest{})
	}

	require.NoError(t, unmarshal(p.MaxRowsPerValidator(), p.MerkleProofDepth()))
	require.ErrorContains(t, unmarshal(p.MaxRowsPerValidator()+1, 0), "rows")
	require.ErrorContains(t, unmarshal(1, p.MerkleProofDepth()+1), "proof segments")
}
