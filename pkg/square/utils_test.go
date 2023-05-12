package square_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/stretchr/testify/assert"
)

func TestEstimateMaxBlockSize(t *testing.T) {
	type test struct {
		squareSize uint64
		expect     int64
	}

	tests := []test{
		{squareSize: 64, expect: 1835520},
		{squareSize: appconsts.MaxSquareSize, expect: 7342080},
		{squareSize: 256, expect: 29368320},
		{squareSize: 512, expect: 117473280},
	}
	for _, tc := range tests {
		res := square.EstimateMaxBlockBytes(tc.squareSize)
		assert.Equal(t, tc.expect, res)

		// check that the result is within the bounds of the square size
		sharesUsed := shares.SparseSharesNeeded(uint32(res))
		roundTripSquareSize := square.Size(int(sharesUsed))
		assert.Equal(t, tc.squareSize, roundTripSquareSize)
	}
}
