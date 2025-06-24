package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/celestiaorg/celestia-app/v4/app"
)

func Test_isKnownChainID(t *testing.T) {
	type testCase struct {
		chainID string
		want    bool
	}
	testCases := []testCase{
		{"celestia", true},
		{"mocha-4", true},
		{"arabica-10", true},
		{"foo", false},
	}

	for _, tc := range testCases {
		t.Run(tc.chainID, func(t *testing.T) {
			got := app.IsKnownChainID(tc.chainID)
			assert.Equal(t, tc.want, got)
		})
	}
}
