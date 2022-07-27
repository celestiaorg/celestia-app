package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNextLowestPowerOf2(t *testing.T) {
	type test struct {
		input    uint64
		expected uint64
	}
	tests := []test{
		{
			input:    2,
			expected: 2,
		},
		{
			input:    11,
			expected: 8,
		},
		{
			input:    511,
			expected: 256,
		},
		{
			input:    1,
			expected: 1,
		},
		{
			input:    0,
			expected: 0,
		},
	}
	for _, tt := range tests {
		res := NextLowestPowerOf2(tt.input)
		assert.Equal(t, tt.expected, res)
	}
}

func TestNextHighestPowerOf2(t *testing.T) {
	type test struct {
		input    uint64
		expected uint64
	}
	tests := []test{
		{
			input:    2,
			expected: 4,
		},
		{
			input:    11,
			expected: 16,
		},
		{
			input:    511,
			expected: 512,
		},
		{
			input:    1,
			expected: 2,
		},
		{
			input:    0,
			expected: 0,
		},
	}
	for _, tt := range tests {
		res := NextHighestPowerOf2(tt.input)
		assert.Equal(t, tt.expected, res)
	}
}

func TestPowerOf2(t *testing.T) {
	type test struct {
		input    uint64
		expected bool
	}
	tests := []test{
		{
			input:    1,
			expected: true,
		},
		{
			input:    2,
			expected: true,
		},
		{
			input:    256,
			expected: true,
		},
		{
			input:    3,
			expected: false,
		},
		{
			input:    79,
			expected: false,
		},
		{
			input:    0,
			expected: false,
		},
	}
	for _, tt := range tests {
		res := IsPowerOf2(tt.input)
		assert.Equal(t, tt.expected, res)
	}
}
