package appconsts_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
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
			name:                       "the upgrade delay for corto",
			chainID:                    appconsts.CortoChainID,
			expectedUpgradeHeightDelay: appconsts.CortoUpgradeHeightDelay,
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

func TestGetConsensusTimeouts(t *testing.T) {
	v10 := appconsts.ConsensusTimeouts{
		TimeoutPropose:          3000 * time.Millisecond,
		TimeoutProposeDelta:     500 * time.Millisecond,
		TimeoutPrevote:          2000 * time.Millisecond,
		TimeoutPrevoteDelta:     500 * time.Millisecond,
		TimeoutPrecommit:        3000 * time.Millisecond,
		TimeoutPrecommitDelta:   500 * time.Millisecond,
		TimeoutCommit:           500 * time.Millisecond,
		DelayedPrecommitTimeout: 2100 * time.Millisecond,
	}
	v11 := appconsts.ConsensusTimeouts{
		TimeoutPropose:        1500 * time.Millisecond,
		TimeoutProposeDelta:   500 * time.Millisecond,
		TimeoutPrevote:        1000 * time.Millisecond,
		TimeoutPrevoteDelta:   500 * time.Millisecond,
		TimeoutPrecommit:      1500 * time.Millisecond,
		TimeoutPrecommitDelta: 500 * time.Millisecond,
		TimeoutCommit:         time.Nanosecond,
		DynamicTimeoutCommit:  800 * time.Millisecond,
	}
	for version, want := range map[uint64]appconsts.ConsensusTimeouts{1: v10, 9: v10, 10: v10, 11: v11, 12: v11} {
		require.Equal(t, want, appconsts.GetConsensusTimeouts(version), "app version %d", version)
	}
	require.Equal(t, 2600*time.Millisecond, v10.BlockInterval())
	require.Equal(t, 800*time.Millisecond, v11.BlockInterval())
}
