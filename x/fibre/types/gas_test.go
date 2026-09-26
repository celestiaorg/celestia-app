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

func TestEstimateGasForPayForFibreSignatureVerification(t *testing.T) {
	tests := []struct {
		name                    string
		validatorSignatureCount uint64
		want                    uint64
	}{
		{
			name:                    "zero signatures is fixed cost only",
			validatorSignatureCount: 0,
			want:                    appconsts.PFFibreTxGasFixedCost,
		},
		{
			name:                    "one signature",
			validatorSignatureCount: 1,
			want: appconsts.PFFibreTxGasFixedCost +
				appconsts.PFFibreGasPerValidatorSignature,
		},
		{
			name:                    "many signatures",
			validatorSignatureCount: 67,
			want: appconsts.PFFibreTxGasFixedCost +
				67*appconsts.PFFibreGasPerValidatorSignature,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, types.EstimateGasForPayForFibreSignatureVerification(tt.validatorSignatureCount))
		})
	}
}

func TestPaymentAmount(t *testing.T) {
	const chunk = appconsts.PFBFibreChunkSize
	for _, tt := range []struct {
		name string
		size uint32
		want int64
	}{
		{"zero", 0, 2_600},
		{"one byte", 1, 2_780},
		{"one chunk", chunk, 2_780},
		{"chunk boundary", chunk + 1, 2_960},
		{"1 MiB payload padded", 5 * chunk, 3_500},
		{"10 MiB payload padded", 41 * chunk, 9_980},
		{"100 MiB payload padded", 401 * chunk, 74_780},
		{"128 MiB upload", 128 << 20, 94_760},
		{"max uint32", ^uint32(0), 2_951_720},
	} {
		t.Run(tt.name, func(t *testing.T) {
			amount := types.PaymentAmount(tt.size)
			require.Equal(t, appconsts.BondDenom, amount.Denom)
			require.Equal(t, tt.want, amount.Amount.Int64())
		})
	}
}
