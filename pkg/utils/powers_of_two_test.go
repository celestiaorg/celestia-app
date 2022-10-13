package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoundUpPowerOfTwo(t *testing.T) {
	type testCase struct {
		input int
		want  int
	}
	testCases := []testCase{
		{input: 1, want: 1},
		{input: 2, want: 2},
		{input: 5, want: 8},
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
		{input: 0, want: 0},
		{input: 1, want: 1},
		{input: 2, want: 2},
		{input: 5, want: 4},
		{input: 11, want: 8},
		{input: 511, want: 256},
	}
	for _, tc := range testCases {
		got := RoundDownPowerOfTwo(tc.input)
		assert.Equal(t, tc.want, got)
	}
}

func TestRoundUpPowerOfTwoU(t *testing.T) {
	type testCase struct {
		input uint64
		want  uint64
	}
	testCases := []testCase{
		{input: 0, want: 0},
		{input: 1, want: 2},
		{input: 2, want: 4},
		{input: 5, want: 8},
		{input: 11, want: 16},
		{input: 511, want: 512},
	}
	for _, tc := range testCases {
		got := RoundUpPowerOfTwoU(tc.input)
		assert.Equal(t, tc.want, got)
	}
}

func TestRoundDownPowerOfTwoU(t *testing.T) {
	type testCase struct {
		input uint64
		want  uint64
	}
	testCases := []testCase{
		{input: 1, want: 1},
		{input: 2, want: 2},
		{input: 5, want: 4},
	}
	for _, tc := range testCases {
		got := RoundDownPowerOfTwoU(tc.input)
		assert.Equal(t, tc.want, got)
	}
}

func TestIsPowerOfTwoU(t *testing.T) {
	type test struct {
		input uint64
		want  bool
	}
	tests := []test{
		{input: 1, want: true},
		{input: 2, want: true},
		{input: 256, want: true},
		{input: 3, want: false},
		{input: 79, want: false},
		{input: 0, want: false},
	}
	for _, tt := range tests {
		got := IsPowerOfTwoU(tt.input)
		assert.Equal(t, tt.want, got)
	}
}
