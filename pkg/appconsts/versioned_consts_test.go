package appconsts_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	v5 "github.com/celestiaorg/celestia-app/v6/pkg/appconsts/v5"
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

func TestGetTimeoutCommit(t *testing.T) {
	tests := []struct {
		appVersion uint64
		want       time.Duration
	}{
		{
			appVersion: 1,
			want:       0,
		},
		{
			appVersion: 2,
			want:       0,
		},
		{
			appVersion: 3,
			want:       v5.TimeoutCommit,
		},
		{
			appVersion: 4,
			want:       v5.TimeoutCommit,
		},
		{
			appVersion: 5,
			want:       v5.TimeoutCommit,
		},
		{
			appVersion: 6,
			want:       appconsts.TimeoutCommit,
		},
		{
			appVersion: 7,
			want:       appconsts.TimeoutCommit,
		},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("the timeout commit for v%d", tc.appVersion), func(t *testing.T) {
			got := appconsts.GetTimeoutCommit(tc.appVersion)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestGetTimeoutPropose(t *testing.T) {
	tests := []struct {
		appVersion uint64
		want       time.Duration
	}{
		{
			appVersion: 1,
			want:       0,
		},
		{
			appVersion: 2,
			want:       0,
		},
		{
			appVersion: 3,
			want:       v5.TimeoutPropose,
		},
		{
			appVersion: 4,
			want:       v5.TimeoutPropose,
		},
		{
			appVersion: 5,
			want:       v5.TimeoutPropose,
		},
		{
			appVersion: 6,
			want:       appconsts.TimeoutPropose,
		},
		{
			appVersion: 7,
			want:       appconsts.TimeoutPropose,
		},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("the timeout propose for v%d", tc.appVersion), func(t *testing.T) {
			got := appconsts.GetTimeoutPropose(tc.appVersion)
			require.Equal(t, tc.want, got)
		})
	}
}
