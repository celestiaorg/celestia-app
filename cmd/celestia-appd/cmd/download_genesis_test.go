package cmd

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/stretchr/testify/assert"
)

func Test_isKnownChainID(t *testing.T) {
	type testCase struct {
		chainID string
		want    bool
	}
	testCases := []testCase{
		{appconsts.MainnetChainID, true},
		{appconsts.MochaChainID, true},
		{"arabica-10", true},
		{"foo", false},
	}

	for _, tc := range testCases {
		t.Run(tc.chainID, func(t *testing.T) {
			got := isKnownChainID(tc.chainID)
			assert.Equal(t, tc.want, got)
		})
	}
}
