package square_test

import (
	"fmt"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/stretchr/testify/require"
)

func BenchmarkSquareConstruct(b *testing.B) {
	rand := tmrand.NewRand()
	for _, txCount := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("txCount=%d", txCount), func(b *testing.B) {
			txs := generateOrderedTxs(rand, txCount/2, txCount/2, 1, 1024)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := square.Construct(txs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkSquareBuild(b *testing.B) {
	rand := tmrand.NewRand()
	for _, txCount := range []int{10, 100, 1000, 10000} {
		b.Run(fmt.Sprintf("txCount=%d", txCount), func(b *testing.B) {
			txs := generateMixedTxs(rand, txCount/2, txCount/2, 1, 1024)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, err := square.Build(txs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
				require.NoError(b, err)
			}
		})
	}
	const txCount = 10
	for _, blobSize := range []int{10, 100, 1000, 10000} {
		b.Run(fmt.Sprintf("blobSize=%d", blobSize), func(b *testing.B) {
			txs := generateMixedTxs(rand, 0, txCount, 1, blobSize)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, err := square.Build(txs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
				require.NoError(b, err)
			}
		})
	}
}
