package types_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/stretchr/testify/require"
)

// BenchmarkParsePayForFibreOverBlobTx measures the parse over the shape that
// dominates a real block and is never a pay-for-fibre tx. Every caller that
// scans a whole block walks these, PreBlocker's warming pass most of all, so
// what this costs is paid per block on every node.
func BenchmarkParsePayForFibreOverBlobTx(b *testing.B) {
	for _, size := range []int{100 << 10, 500 << 10, 1 << 20} {
		raw := blobTxOfSize(b, size)
		b.Run(fmt.Sprintf("blob=%dKiB", size>>10), func(b *testing.B) {
			b.SetBytes(int64(len(raw)))
			for b.Loop() {
				if _, isPFF, _ := types.ParsePayForFibreTx(raw); isPFF {
					b.Fatal("a blob tx is never a pay-for-fibre tx")
				}
			}
		})
	}
}

// blobTxOfSize builds a BlobTx carrying one blob of size bytes.
func blobTxOfSize(b testing.TB, size int) []byte {
	b.Helper()
	blob, err := share.NewV0Blob(share.RandomBlobNamespace(), bytes.Repeat([]byte{0xab}, size))
	require.NoError(b, err)
	raw, err := squaretx.MarshalBlobTx([]byte("inner-tx-bytes"), blob)
	require.NoError(b, err)
	return raw
}
