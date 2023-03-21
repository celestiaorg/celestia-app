package shares

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundUpPowerOfTwo(t *testing.T) {
	type testCase struct {
		input int
		want  int
	}
	testCases := []testCase{
		{input: -1, want: 1},
		{input: 0, want: 1},
		{input: 1, want: 1},
		{input: 2, want: 2},
		{input: 4, want: 4},
		{input: 5, want: 8},
		{input: 8, want: 8},
		{input: 11, want: 16},
		{input: 511, want: 512},
	}
	for _, tc := range testCases {
		got := RoundUpPowerOfTwo(tc.input)
		assert.Equal(t, tc.want, got)
	}
}

func TestRoundDownPowerOfTwo(t *testing.T) {
	type testCase struct {
		input int
		want  int
	}
	testCases := []testCase{
		{input: 1, want: 1},
		{input: 2, want: 2},
		{input: 4, want: 4},
		{input: 5, want: 4},
		{input: 8, want: 8},
		{input: 11, want: 8},
		{input: 511, want: 256},
	}
	for _, tc := range testCases {
		got, err := RoundDownPowerOfTwo(tc.input)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
}

func TestRoundUpPowerOfTwoStrict(t *testing.T) {
	type testCase struct {
		input int
		want  int
	}
	testCases := []testCase{
		{input: -1, want: 1},
		{input: 0, want: 1},
		{input: 1, want: 2},
		{input: 2, want: 4},
		{input: 4, want: 8},
		{input: 5, want: 8},
		{input: 8, want: 16},
		{input: 11, want: 16},
		{input: 511, want: 512},
	}
	for _, tc := range testCases {
		got := RoundUpPowerOfTwoStrict(tc.input)
		assert.Equal(t, tc.want, got)
	}
}

func TestIsPowerOfTwoU(t *testing.T) {
	type test struct {
		input uint64
		want  bool
	}
	tests := []test{
		// powers of two
		{input: 1, want: true},
		{input: 2, want: true},
		{input: 4, want: true},
		{input: 8, want: true},
		{input: 16, want: true},
		{input: 32, want: true},
		{input: 64, want: true},
		{input: 128, want: true},
		{input: 256, want: true},
		// not powers of two
		{input: 0, want: false},
		{input: 3, want: false},
		{input: 12, want: false},
		{input: 79, want: false},
	}
	for _, tt := range tests {
		got := IsPowerOfTwo(tt.input)
		assert.Equal(t, tt.want, got)
	}
}
