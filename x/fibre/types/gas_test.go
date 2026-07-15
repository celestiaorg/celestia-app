package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
)

func TestEstimateGasForPayForFibre(t *testing.T) {
	const chunk = appconsts.PFBFibreChunkSize // 256 KiB
	tests := []struct {
		name     string
		blobSize uint32
		want     uint64
	}{
		{"zero is fixed cost only", 0, appconsts.PFBFibreGasFixedCost},
		{"one byte is one chunk", 1, appconsts.PFBFibreGasFixedCost + appconsts.PFBFibreGasPerChunk},
		{"exactly one chunk", chunk, appconsts.PFBFibreGasFixedCost + appconsts.PFBFibreGasPerChunk},
		{"one chunk plus one byte rounds up to two", chunk + 1, appconsts.PFBFibreGasFixedCost + 2*appconsts.PFBFibreGasPerChunk},
		{"exactly two chunks", 2 * chunk, appconsts.PFBFibreGasFixedCost + 2*appconsts.PFBFibreGasPerChunk},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, types.EstimateGasForPayForFibre(tt.blobSize))
		})
	}
}

func TestPaymentAmount(t *testing.T) {
	const blobSize = 5 * appconsts.PFBFibreChunkSize
	amount := types.PaymentAmount(blobSize)
	require.Equal(t, appconsts.BondDenom, amount.Denom)
	require.Equal(t, int64(types.EstimateGasForPayForFibre(blobSize)), amount.Amount.Int64())
}
