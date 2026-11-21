package proof_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/da"
	"github.com/celestiaorg/celestia-app/v11/pkg/proof"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/stretchr/testify/require"
)

// TestNewShareInclusionProofFromEDSRejectsBadRange checks that ranges outside
// the original data square return an error instead of panicking or building a
// proof that Validate rejects.
func TestNewShareInclusionProofFromEDSRejectsBadRange(t *testing.T) {
	const width = 4 // original data square is 4x4: shares [0, 16)
	eds, err := da.ExtendShares(share.ToBytes(share.TailPaddingShares(width * width)))
	require.NoError(t, err)

	for name, r := range map[string]share.Range{
		"empty range":           {Start: 3, End: 3},
		"end before start":      {Start: 5, End: 2},
		"negative start":        {Start: -1, End: 2},
		"end past the square":   {Start: 10, End: width*width + 1},
		"start past the square": {Start: width * width, End: width*width + 2},
		"far past the square":   {Start: 0, End: 1 << 20},
	} {
		t.Run(name, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := proof.NewShareInclusionProofFromEDS(eds, share.TxNamespace, r)
				require.Error(t, err)
			})
		})
	}

	t.Run("nil EDS", func(t *testing.T) {
		require.NotPanics(t, func() {
			_, err := proof.NewShareInclusionProofFromEDS(nil, share.TxNamespace, share.NewRange(0, 1))
			require.Error(t, err)
		})
	})
}
