package types

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/stretchr/testify/assert"
)

func Test_validateGovMaxSquareSize(t *testing.T) {
	type test struct {
		name      string
		input     any
		expectErr bool
	}
	tests := []test{
		{
			name:      "valid",
			input:     uint64(appconsts.DefaultGovMaxSquareSize),
			expectErr: false,
		},
		{
			name:      "not a power of 2",
			input:     uint64(appconsts.SquareSizeUpperBound - 1),
			expectErr: true,
		},
		{
			name:      "wrong type",
			input:     int64(appconsts.DefaultGovMaxSquareSize),
			expectErr: true,
		},
		{
			name:      "zero",
			input:     uint64(0),
			expectErr: true,
		},
		{
			// Without the upper-bound guard, downstream uint64 multiplications
			// (governMaxSquareSize^2 * share.ShareSize) silently wrap.
			name:      "exceeds upper bound",
			input:     uint64(appconsts.SquareSizeUpperBound) * 2,
			expectErr: true,
		},
		{
			name:      "exactly at upper bound",
			input:     uint64(appconsts.SquareSizeUpperBound),
			expectErr: false,
		},
	}
	for _, tt := range tests {
		err := validateGovMaxSquareSize(tt.input)
		if tt.expectErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}
