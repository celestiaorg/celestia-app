package appconsts_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/stretchr/testify/require"
)

func TestGetUpgradeHeightDelay(t *testing.T) {
	tests := []struct {
		name                       string
		chainID                    string
		expectedUpgradeHeightDelay int64
	}{
		{
			name:                       "the upgrade delay for chainID test",
			chainID:                    appconsts.TestChainID,
			expectedUpgradeHeightDelay: appconsts.TestUpgradeHeightDelay,
		},
		{
			name:                       "the upgrade delay for arabica",
			chainID:                    appconsts.ArabicaChainID,
			expectedUpgradeHeightDelay: appconsts.ArabicaUpgradeHeightDelay,
		},
		{
			name:                       "the upgrade delay for mocha",
			chainID:                    appconsts.MochaChainID,
			expectedUpgradeHeightDelay: appconsts.MochaUpgradeHeightDelay,
		},
		{
			name:                       "the upgrade delay for mainnet",
			chainID:                    appconsts.MainnetChainID,
			expectedUpgradeHeightDelay: appconsts.MainnetUpgradeHeightDelay,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := appconsts.GetUpgradeHeightDelay(tc.chainID)
			require.Equal(t, tc.expectedUpgradeHeightDelay, got)
		})
	}
}
