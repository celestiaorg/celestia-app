package appconsts_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v2"
	v3 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v4"
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
			name:             "MaxTxSize v3",
			version:          v3.Version,
			expectedConstant: v3.MaxTxSize,
			got:              appconsts.MaxTxSize(v3.Version),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedConstant, tc.got)
		})
	}
}

func TestUpgradeHeightDelay(t *testing.T) {
	tests := []struct {
		name                       string
		chainID                    string
		version                    uint64
		expectedUpgradeHeightDelay int64
	}{
		{
			name:                       "v1 upgrade delay",
			chainID:                    "test-chain",
			version:                    v1.Version,
			expectedUpgradeHeightDelay: v1.UpgradeHeightDelay,
		},
		{
			name:                       "v1 arabica upgrade delay",
			chainID:                    "arabica-11",
			version:                    v1.Version,
			expectedUpgradeHeightDelay: v1.UpgradeHeightDelay,
		},
		{
			name:                       "v2 upgrade delay on non-arabica chain",
			chainID:                    "celestia",
			version:                    v2.Version,
			expectedUpgradeHeightDelay: v2.UpgradeHeightDelay,
		},
		{
			name:                       "v2 upgrade delay on arabica",
			chainID:                    "arabica-11",
			version:                    v2.Version,
			expectedUpgradeHeightDelay: v3.UpgradeHeightDelay, // falls back to v3 because of arabica bug
		},
		{
			name:                       "v3 upgrade delay",
			chainID:                    "mocha-4",
			version:                    3,
			expectedUpgradeHeightDelay: v3.UpgradeHeightDelay,
		},
		{
			name:                       "the upgrade delay for chainID 'test' should be 3 regardless of the version",
			chainID:                    appconsts.TestChainID,
			version:                    3,
			expectedUpgradeHeightDelay: 3,
		},
		{
			name:                       "the upgrade delay for chainID 'test' should be 3 regardless of the version",
			chainID:                    appconsts.TestChainID,
			version:                    4,
			expectedUpgradeHeightDelay: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := appconsts.UpgradeHeightDelay(tc.chainID, tc.version)
			require.Equal(t, tc.expectedUpgradeHeightDelay, actual)
		})
	}
}
