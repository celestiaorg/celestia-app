package abci

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetForAppVersion(t *testing.T) {
	tests := []struct {
		name        string
		versions    Versions
		appVersion  uint64
		expected    Version
		expectedErr error
	}{
		{
			name: "exact match",
			versions: Versions{
				{AppVersion: 1},
				{AppVersion: 2},
				{AppVersion: 3},
			},
			appVersion: 2,
			expected:   Version{AppVersion: 2},
		},
		{
			name: "app version matches the lowest version",
			versions: Versions{
				{AppVersion: 1},
				{AppVersion: 2},
			},
			appVersion: 1,
			expected:   Version{AppVersion: 1},
		},
		{
			name: "app version matches the highest version",
			versions: Versions{
				{AppVersion: 1},
				{AppVersion: 2},
			},
			appVersion: 2,
			expected:   Version{AppVersion: 2},
		},
		{
			name:        "empty versions list returns ErrNoVersionFound",
			versions:    Versions{},
			appVersion:  1,
			expectedErr: ErrNoVersionFound,
		},
		{
			name: "app version above the highest version returns ErrNoVersionFound",
			versions: Versions{
				{AppVersion: 1},
				{AppVersion: 2},
				{AppVersion: 3},
			},
			appVersion:  4,
			expectedErr: ErrNoVersionFound,
		},
		{
			name: "app version below the lowest version returns ErrUnsupportedAppVersion",
			versions: Versions{
				{AppVersion: 2},
				{AppVersion: 3},
			},
			appVersion:  1,
			expectedErr: ErrUnsupportedAppVersion,
		},
		{
			name: "app version far below the lowest version returns ErrUnsupportedAppVersion",
			versions: Versions{
				{AppVersion: 4},
				{AppVersion: 5},
				{AppVersion: 6},
			},
			appVersion:  2,
			expectedErr: ErrUnsupportedAppVersion,
		},
		{
			name: "app version in a gap between registered versions returns ErrUnsupportedAppVersion",
			versions: Versions{
				{AppVersion: 1},
				{AppVersion: 3},
			},
			appVersion:  2,
			expectedErr: ErrUnsupportedAppVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := tt.versions.GetForAppVersion(tt.appVersion)

			if tt.expectedErr != nil {
				require.ErrorIs(t, err, tt.expectedErr)
				require.ErrorContains(t, err, fmt.Sprintf("app version %d", tt.appVersion), "error should name the offending app version")
				require.Equal(t, Version{}, actual)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, actual, "unexpected result")
			}
		})
	}
}

func TestShouldUseLatestApp(t *testing.T) {
	tests := []struct {
		name       string
		versions   Versions
		appVersion uint64
		expected   bool
	}{
		{"No versions available", Versions{}, 1, true},
		{
			"App version matches the first version",
			Versions{
				{AppVersion: 1},
				{AppVersion: 2},
			},
			1, false,
		},
		{
			"App version matches a later version",
			Versions{
				{AppVersion: 1},
				{AppVersion: 2},
			},
			2, false,
		},
		{
			"App version above every registered version",
			Versions{
				{AppVersion: 1},
				{AppVersion: 2},
			},
			3, true,
		},
		{
			"App version below the lowest registered version is not served by the latest app",
			Versions{
				{AppVersion: 2},
				{AppVersion: 3},
			},
			1, false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.versions.ShouldUseLatestApp(tt.appVersion))
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		versions    Versions
		expectedErr string
	}{
		{
			name:     "no duplicates",
			versions: []Version{{AppVersion: 1}, {AppVersion: 2}, {AppVersion: 3}},
		},
		{
			name:        "duplicate app versions",
			versions:    []Version{{AppVersion: 1}, {AppVersion: 2}, {AppVersion: 1}},
			expectedErr: "version 1 specified multiple times",
		},
		{
			name:        "empty list",
			versions:    []Version{},
			expectedErr: "no versions specified",
		},
		{
			name:     "single element",
			versions: []Version{{AppVersion: 1}},
		},
		{
			name:        "multiple duplicates",
			versions:    []Version{{AppVersion: 1}, {AppVersion: 2}, {AppVersion: 1}, {AppVersion: 3}, {AppVersion: 2}},
			expectedErr: "version 1 specified multiple times",
		},
		{
			name:        "gap between registered versions",
			versions:    []Version{{AppVersion: 1}, {AppVersion: 3}},
			expectedErr: "version 2 is missing",
		},
		{
			name:        "gap in the middle of a longer range",
			versions:    []Version{{AppVersion: 3}, {AppVersion: 4}, {AppVersion: 6}, {AppVersion: 7}},
			expectedErr: "version 5 is missing",
		},
		{
			name:     "contiguous but unsorted",
			versions: []Version{{AppVersion: 3}, {AppVersion: 1}, {AppVersion: 2}},
		},
		{
			name:     "contiguous range not starting at 1",
			versions: []Version{{AppVersion: 3}, {AppVersion: 4}, {AppVersion: 5}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.versions.Validate()

			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
