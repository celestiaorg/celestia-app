package appconsts_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/stretchr/testify/require"
)

func TestUpgradeHeightDelay(t *testing.T) {
	tests := []struct {
		name                       string
		chainID                    string
		expectedUpgradeHeightDelay int64
	}{
		{
			name:                       "the upgrade delay for chainID 'test' should be 3",
			chainID:                    appconsts.TestChainID,
			expectedUpgradeHeightDelay: 3,
		},
		{
			name:                       "the upgrade delay should be latest value",
			chainID:                    "arabica-11",
			expectedUpgradeHeightDelay: appconsts.UpgradeHeightDelay,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := appconsts.GetUpgradeHeightDelay(tc.chainID)
			require.Equal(t, tc.expectedUpgradeHeightDelay, actual)
		})
	}
}
