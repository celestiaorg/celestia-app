package fibre

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
)

func TestParseRowRejectsNonCanonicalProofSegments(t *testing.T) {
	canonicalSeg := make([]byte, merkle.NodeSize)

	t.Run("canonical proof accepted", func(t *testing.T) {
		row := &types.BlobRow{
			Index: 0,
			Data:  []byte("row-data"),
			Proof: [][]byte{canonicalSeg},
		}
		_, err := parseRow(row)
		require.NoError(t, err)
	})

	t.Run("oversized segment rejected", func(t *testing.T) {
		// A correct 32-byte prefix plus attacker padding: verification would
		// truncate it back to a valid node, but it must never be stored.
		padded := append(append([]byte(nil), canonicalSeg...), 0xAA, 0xBB)
		row := &types.BlobRow{
			Index: 3,
			Data:  []byte("row-data"),
			Proof: [][]byte{padded},
		}
		_, err := parseRow(row)
		require.Error(t, err)
	})

	t.Run("undersized segment rejected", func(t *testing.T) {
		row := &types.BlobRow{
			Index: 3,
			Data:  []byte("row-data"),
			Proof: [][]byte{canonicalSeg[:merkle.NodeSize-1]},
		}
		_, err := parseRow(row)
		require.Error(t, err)
	})
}
