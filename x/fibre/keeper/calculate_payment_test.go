package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
)

func TestEstimateGasForPayForFibre(t *testing.T) {
	tests := []struct {
		name     string
		blobSize uint32
		want     uint64
	}{
		{
			name:     "zero blob size returns fixed cost only",
			blobSize: 0,
			want:     650_000,
		},
		{
			name:     "1 byte = 1 chunk",
			blobSize: 1,
			want:     650_000 + 45_000,
		},
		{
			name:     "exactly 256 KiB = 1 chunk",
			blobSize: 262_144,
			want:     650_000 + 45_000,
		},
		{
			name:     "256 KiB + 1 byte = 2 chunks",
			blobSize: 262_145,
			want:     650_000 + 2*45_000,
		},
		{
			name:     "exactly 512 KiB = 2 chunks",
			blobSize: 524_288,
			want:     650_000 + 2*45_000,
		},
		{
			name:     "1 MiB = 4 chunks",
			blobSize: 1_048_576,
			want:     650_000 + 4*45_000,
		},
		{
			name:     "8 MiB = 32 chunks",
			blobSize: 8_388_608,
			want:     650_000 + 32*45_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateGasForPayForFibre(tt.blobSize)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCalculatePaymentAmount(t *testing.T) {
	tests := []struct {
		name     string
		blobSize uint32
		want     sdk.Coin
	}{
		{
			name:     "zero blob size",
			blobSize: 0,
			want:     sdk.NewCoin(appconsts.BondDenom, sdkmath.NewIntFromUint64(650_000)),
		},
		{
			name:     "1 byte blob",
			blobSize: 1,
			want:     sdk.NewCoin(appconsts.BondDenom, sdkmath.NewIntFromUint64(695_000)),
		},
		{
			name:     "exactly 256 KiB",
			blobSize: 262_144,
			want:     sdk.NewCoin(appconsts.BondDenom, sdkmath.NewIntFromUint64(695_000)),
		},
		{
			name:     "8 MiB blob",
			blobSize: 8_388_608,
			want:     sdk.NewCoin(appconsts.BondDenom, sdkmath.NewIntFromUint64(2_090_000)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gas := EstimateGasForPayForFibre(tt.blobSize)
			got := sdk.NewCoin(appconsts.BondDenom, sdkmath.NewIntFromUint64(gas))
			assert.Equal(t, tt.want, got)
		})
	}
}
