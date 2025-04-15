package abci

import (
	"errors"
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
			appVersion:  2,
			expected:    Version{AppVersion: 2},
			expectedErr: nil,
		},
		{
			name: "no matching version returns smallest available version",
			versions: Versions{
				{AppVersion: 2},
				{AppVersion: 3},
			},
			appVersion:  1,
			expected:    Version{AppVersion: 2},
			expectedErr: nil,
		},
		{
			name:        "empty versions list returns error",
			versions:    Versions{},
			appVersion:  1,
			expected:    Version{},
			expectedErr: fmt.Errorf("%w: %d", ErrNoVersionFound, 1),
		},
		{
			name: "app version matches the lowest version",
			versions: Versions{
				{AppVersion: 1},
				{AppVersion: 3},
			},
			appVersion:  1,
			expected:    Version{AppVersion: 1},
			expectedErr: nil,
		},
		{
			name: "app version not in list, returns lowest",
			versions: Versions{
				{AppVersion: 4},
				{AppVersion: 5},
				{AppVersion: 6},
			},
			appVersion:  2,
			expected:    Version{AppVersion: 4},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := tt.versions.GetForAppVersion(tt.appVersion)

			if tt.expectedErr != nil {
				require.Error(t, err)
				require.EqualError(t, tt.expectedErr, err.Error(), "unexpected error message")
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
			}, 1, false,
		},
		{
			"App version matches a later version",
			Versions{
				{AppVersion: 1},
				{AppVersion: 2},
			}, 2, false,
		},
		{
			"App version does not match any version",
			Versions{
				{AppVersion: 1},
				{AppVersion: 2},
			}, 3, true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.versions.ShouldUseLatestApp(tt.appVersion))
		})
	}
}

func TestEnsureUniqueVersions(t *testing.T) {
	tests := []struct {
		name        string
		versions    Versions
		expectedErr error
	}{
		{
			name:        "no duplicates",
			versions:    []Version{{AppVersion: 1}, {AppVersion: 2}, {AppVersion: 3}},
			expectedErr: nil,
		},
		{
			name:        "duplicate app versions",
			versions:    []Version{{AppVersion: 1}, {AppVersion: 2}, {AppVersion: 1}},
			expectedErr: errors.New("version 1 specified multiple times"),
		},
		{
			name:        "empty list",
			versions:    []Version{},
			expectedErr: errors.New("no versions specified"),
		},
		{
			name:        "single element",
			versions:    []Version{{AppVersion: 1}},
			expectedErr: nil,
		},
		{
			name:        "multiple duplicates",
			versions:    []Version{{AppVersion: 1}, {AppVersion: 2}, {AppVersion: 1}, {AppVersion: 3}, {AppVersion: 2}},
			expectedErr: errors.New("version 1 specified multiple times"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.versions.Validate()

			if tt.expectedErr != nil {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErr.Error(), "expected error message mismatch")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
