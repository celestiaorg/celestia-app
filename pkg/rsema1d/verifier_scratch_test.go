package rsema1d

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/field"
	"github.com/stretchr/testify/require"
)

// TestVerifyClearsScratch checks that Verify drops its references to the
// caller's rows and proofs, including after the batch shrinks, so a pooled
// verifier does not pin the request buffers they alias.
func TestVerifyClearsScratch(t *testing.T) {
	cfg := &Config{K: 16, N: 16, WorkerCount: 1}
	coder, err := NewCoder(cfg)
	require.NoError(t, err)
	rows := make([][]byte, cfg.K+cfg.N)
	for i := range rows {
		rows[i] = bytes.Repeat([]byte{byte(i)}, field.LeopardChunkSize)
	}
	ed, err := coder.Encode(rows)
	require.NoError(t, err)

	v, err := NewVerifier(cfg)
	require.NoError(t, err)
	for _, n := range []int{8, 4} {
		proofs := make([]*RowProof, n)
		for i := range proofs {
			proofs[i], err = ed.GenerateRowProof(i)
			require.NoError(t, err)
		}
		require.NoError(t, v.Verify(ed.Commitment(), proofs, ed.RLC()))
		for i, row := range v.rowsScratch[:cap(v.rowsScratch)] {
			require.Nil(t, row, "n=%d: rowsScratch[%d] still referenced", n, i)
		}
		for i, proof := range v.proofScratch[:cap(v.proofScratch)] {
			require.Zero(t, proof, "n=%d: proofScratch[%d] still referenced", n, i)
		}
	}
}
