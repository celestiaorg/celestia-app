package types

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
)

func Test_validateGovMaxSquareSize(t *testing.T) {
	type test struct {
		name      string
		input     interface{}
		expectErr bool
	}
	tests := []test{
		{
			name:      "valid",
			input:     uint64(appconsts.DefaultGovMaxSquareSize),
			expectErr: false,
		},
		{
			name:      "too big",
			input:     uint64(appconsts.MaxSquareSize * 2),
			expectErr: true,
		},
		{
			name:      "not a power of 2",
			input:     uint64(appconsts.MaxSquareSize - 1),
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
	}
	for _, tt := range tests {
		err := validateGovMaxSquareSize(tt.input)
		if tt.expectErr {
			assert.Error(t, err)
		}
	}
}
