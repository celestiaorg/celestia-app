package proof

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_safeConvertInt64ToInt(t *testing.T) {
	testCases := []struct {
		input int64
		want  int
	}{
		{input: math.MinInt64, want: math.MinInt64},
		{input: -1, want: -1},
		{input: 0, want: 0},
		{input: 1, want: 1},
		{input: math.MaxInt64, want: math.MaxInt64},
	}
	for _, tc := range testCases {
		got, err := safeConvertInt64ToInt(tc.input)
		assert.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
}
