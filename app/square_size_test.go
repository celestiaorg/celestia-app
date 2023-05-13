package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
)

func TestSquareSizeFromMaxBytes(t *testing.T) {
	type test struct {
		input int64
		want  uint64
	}
	tests := []test{
		{input: 0, want: 1},
		{input: appconsts.MaxShareCount * appconsts.ContinuationSparseShareContentSize, want: appconsts.MaxSquareSize},
		{input: appconsts.MaxShareCount*appconsts.ContinuationSparseShareContentSize + 1, want: appconsts.MaxSquareSize},
		{input: appconsts.DefaultMaxBytes, want: appconsts.DefaultGovMaxSquareSize},
	}
	for _, tt := range tests {
		got := SquareSizeFromMaxBytes(tt.input)
		assert.Equal(t, tt.want, got)
	}
}
