package rsema1d

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/rlc"
	"github.com/stretchr/testify/require"
)

func TestVerifierReleasesFailedProofs(t *testing.T) {
	v, err := NewVerifier(&Config{K: 4, N: 4, WorkerCount: 1})
	require.NoError(t, err)
	proof := &RowProof{Index: 0, Row: make([]byte, 64), RowProof: [][]byte{make([]byte, 32), make([]byte, 32), make([]byte, 32)}}
	require.Error(t, v.Verify(Commitment{}, []*RowProof{proof}, make(rlc.Vector, 4)))
	for _, row := range v.rowsScratch {
		require.Nil(t, row)
	}
	for _, p := range v.proofScratch {
		require.Nil(t, p.Leaf)
		require.Nil(t, p.Path)
	}
}
