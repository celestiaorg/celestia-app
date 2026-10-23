package types_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	"github.com/stretchr/testify/require"
)

func TestParseMerkleProofMetadata(t *testing.T) {
	bz := binary.BigEndian.AppendUint32(nil, 0x01020304)
	for i := range util.TreeDepth {
		bz = append(bz, bytes.Repeat([]byte{byte(i)}, 32)...)
	}

	decoded, err := types.ParseMerkleProofMetadata(bz)
	require.NoError(t, err)
	require.Equal(t, uint32(0x01020304), decoded.Index)
	for i, node := range decoded.Branch {
		require.Equal(t, bytes.Repeat([]byte{byte(i)}, 32), node[:])
	}
}

func TestParseMerkleProofMetadataRejectsInvalidLength(t *testing.T) {
	for _, n := range []int{0, 4, types.MerkleProofMetadataBytes - 1, types.MerkleProofMetadataBytes + 1} {
		_, err := types.ParseMerkleProofMetadata(make([]byte, n))
		require.Error(t, err, "expected error on %d-byte input", n)
	}
}
