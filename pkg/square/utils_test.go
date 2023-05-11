package square

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/stretchr/testify/assert"
)

func TestEstimateMaxBlockSize(t *testing.T) {
	type test struct {
		squareSize uint64
		expect     int64
	}

	tests := []test{
		{squareSize: 64, expect: 1918731},
		{squareSize: appconsts.MaxSquareSize, expect: 7674921},
		{squareSize: 256, expect: 30699684},
		{squareSize: 512, expect: 122798736},
	}
	for _, tc := range tests {
		res := EstimateMaxBlockBytes(tc.squareSize)
		assert.Equal(t, tc.expect, res)

		// check that the result is within the bounds of the square size
		sharesUsed := shares.SparseSharesNeeded(uint32(res))
		roundTripSquareSize := Size(int(sharesUsed))
		assert.Equal(t, tc.squareSize, roundTripSquareSize)
	}
}
