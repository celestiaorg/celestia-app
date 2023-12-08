package appconsts_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/appconsts/testground"
	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
)

func TestSubtreeRootThreshold(t *testing.T) {
	type testCase struct {
		version uint64
		want    int
	}
	testCases := []testCase{
		{version: v1.Version, want: v1.SubtreeRootThreshold},
		{version: v2.Version, want: v2.SubtreeRootThreshold},
		{version: testground.Version, want: testground.SubtreeRootThreshold},
	}
	for _, tc := range testCases {
		name := fmt.Sprintf("version %v", tc.version)
		t.Run(name, func(t *testing.T) {
			got := appconsts.SubtreeRootThreshold(tc.version)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSquareSizeUpperBound(t *testing.T) {
	type testCase struct {
		version uint64
		want    int
	}
	testCases := []testCase{
		{version: v1.Version, want: v1.SquareSizeUpperBound},
		{version: v2.Version, want: v2.SquareSizeUpperBound},
		{version: testground.Version, want: testground.SquareSizeUpperBound},
	}
	for _, tc := range testCases {
		name := fmt.Sprintf("version %v", tc.version)
		t.Run(name, func(t *testing.T) {
			got := appconsts.SquareSizeUpperBound(tc.version)
			assert.Equal(t, tc.want, got)
		})
	}
}
