package app

import (
	"testing"

	blobtx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/stretchr/testify/require"
)

func TestBlobTxProbePreservesClassification(t *testing.T) {
	for _, rawTx := range [][]byte{
		{0x0a, 0x04, 't', 'e', 's', 't'},
		{0x0a, 0x04, 'B', 'L', 'O', 'B'},
		{0x1a, 0x04, 'B', 'L', 'O', 'B'},
		{0x1a, 0x84, 0x00, 'B', 'L', 'O', 'B'},
	} {
		originalTx, originalBlob, originalErr := blobtx.UnmarshalBlobTx(rawTx)
		probedTx, probedBlob, probedErr := unmarshalBlobTxIfPresent(rawTx)
		require.Equal(t, originalBlob, probedBlob)
		if originalBlob {
			require.Equal(t, originalTx, probedTx)
			require.Equal(t, originalErr, probedErr)
		}
	}
}
