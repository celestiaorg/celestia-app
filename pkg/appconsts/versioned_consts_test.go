package appconsts_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
)

func TestVersionedConsts(t *testing.T) {
	testCases := []struct {
		name             string
		version          uint64
		expectedConstant interface{}
		got              interface{}
	}{
		{
			name:             "SubtreeRootThreshold v1",
			version:          v1.Version,
			expectedConstant: v1.SubtreeRootThreshold,
			got:              appconsts.SubtreeRootThreshold(v1.Version),
		},
		{
			name:             "SubtreeRootThreshold v2",
			version:          v2.Version,
			expectedConstant: v2.SubtreeRootThreshold,
			got:              appconsts.SubtreeRootThreshold(v2.Version),
		},
		{
			name:             "SubtreeRootThreshold v3",
			version:          v3.Version,
			expectedConstant: v3.SubtreeRootThreshold,
			got:              appconsts.SubtreeRootThreshold(v3.Version),
		},
		{
			name:             "SquareSizeUpperBound v1",
			version:          v1.Version,
			expectedConstant: v1.SquareSizeUpperBound,
			got:              appconsts.SquareSizeUpperBound(v1.Version),
		},
		{
			name:             "SquareSizeUpperBound v2",
			version:          v2.Version,
			expectedConstant: v2.SquareSizeUpperBound,
			got:              appconsts.SquareSizeUpperBound(v2.Version),
		},
		{
			name:             "SquareSizeUpperBound v3",
			version:          v3.Version,
			expectedConstant: v3.SquareSizeUpperBound,
			got:              appconsts.SquareSizeUpperBound(v3.Version),
		},
		{
			name:             "TxSizeCostPerByte v3",
			version:          v3.Version,
			expectedConstant: v3.TxSizeCostPerByte,
			got:              appconsts.TxSizeCostPerByte(v3.Version),
		},
		{
			name:             "GasPerBlobByte v3",
			version:          v3.Version,
			expectedConstant: v3.GasPerBlobByte,
			got:              appconsts.GasPerBlobByte(v3.Version),
		},
		{
			name:             "MaxTxBytes v3",
			version:          v3.Version,
			expectedConstant: v3.MaxTxBytes,
			got:              appconsts.TxMaxBytes(v3.Version),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedConstant, tc.got)
		})
	}
}
