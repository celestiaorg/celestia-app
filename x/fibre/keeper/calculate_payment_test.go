package keeper_test

import (
	"math"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
)

func TestCalculatePaymentAmount(t *testing.T) {
	tests := []struct {
		name           string
		blobSize       uint32
		gasPerBlobByte uint32
		want           sdk.Coin
	}{
		{
			name:           "normal case",
			blobSize:       1000,
			gasPerBlobByte: 8,
			want:           sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(8000)),
		},
		{
			name:           "overflow case: product exceeds uint32 max",
			blobSize:       8_388_608, // 8 MiB
			gasPerBlobByte: 1000,
			want:           sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(8_388_608_000)),
		},
		{
			name:           "large values near uint32 max",
			blobSize:       math.MaxUint32,
			gasPerBlobByte: 2,
			want:           sdk.NewCoin(appconsts.BondDenom, sdkmath.NewIntFromUint64(2*uint64(math.MaxUint32))),
		},
		{
			name:           "max uint32 * max uint32 does not overflow",
			blobSize:       math.MaxUint32,
			gasPerBlobByte: math.MaxUint32,
			want:           sdk.NewCoin(appconsts.BondDenom, sdkmath.NewIntFromUint64(uint64(math.MaxUint32)*uint64(math.MaxUint32))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keeper.CalculatePaymentAmount(tt.blobSize, tt.gasPerBlobByte)
			assert.Equal(t, tt.want, got)
		})
	}
}
