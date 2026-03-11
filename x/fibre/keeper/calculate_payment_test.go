package keeper_test

import (
	"math"
	"testing"

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
			want:           sdk.NewInt64Coin(appconsts.BondDenom, 8000),
		},
		{
			name:           "overflow case: product exceeds uint32 max",
			blobSize:       8_388_608, // 8 MiB
			gasPerBlobByte: 1000,
			want:           sdk.NewInt64Coin(appconsts.BondDenom, 8_388_608_000),
		},
		{
			name:           "large values near uint32 max",
			blobSize:       math.MaxUint32,
			gasPerBlobByte: 2,
			want:           sdk.NewInt64Coin(appconsts.BondDenom, 2*int64(math.MaxUint32)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keeper.CalculatePaymentAmount(tt.blobSize, tt.gasPerBlobByte)
			assert.Equal(t, tt.want, got)
		})
	}
}
