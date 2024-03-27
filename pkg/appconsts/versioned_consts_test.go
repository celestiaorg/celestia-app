package appconsts_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts/testground"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
)

func TestSubtreeRootThreshold(t *testing.T) {
	testCases := []struct {
		version  uint64
		expected int
	}{
		{
			version:  v1.Version,
			expected: v1.SubtreeRootThreshold,
		},
		{
			version:  v2.Version,
			expected: v2.SubtreeRootThreshold,
		},
		{
			version:  testground.Version,
			expected: testground.SubtreeRootThreshold,
		},
	}

	for _, tc := range testCases {
		name := fmt.Sprintf("version %v", tc.version)
		t.Run(name, func(t *testing.T) {
			got := appconsts.SubtreeRootThreshold(tc.version)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestSquareSizeUpperBound(t *testing.T) {
	testCases := []struct {
		version  uint64
		expected int
	}{
		{
			version:  v1.Version,
			expected: v1.SquareSizeUpperBound,
		},
		{
			version:  v2.Version,
			expected: v2.SquareSizeUpperBound,
		},
		{
			version:  testground.Version,
			expected: testground.SquareSizeUpperBound,
		},
	}

	for _, tc := range testCases {
		name := fmt.Sprintf("version %v", tc.version)
		t.Run(name, func(t *testing.T) {
			got := appconsts.SquareSizeUpperBound(tc.version)
			require.Equal(t, tc.expected, got)
		})
	}
}
