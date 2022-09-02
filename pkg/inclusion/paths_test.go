package inclusion

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_calculateSubTreeRootCoordinates(t *testing.T) {
	type test struct {
		name                 string
		start, end, maxDepth uint64
		expected             []coord
	}
	tests := []test{
		{
			name:     "first four shares of an 8 leaf tree",
			start:    0,
			end:      3,
			maxDepth: 3,
			expected: []coord{
				{
					depth:    1,
					position: 0,
				},
			},
		},
		{
			name:     "second set of four shares of an 8 leaf tree",
			start:    4,
			end:      7,
			maxDepth: 3,
			expected: []coord{
				{
					depth:    1,
					position: 1,
				},
			},
		},
		{
			name:     "middle 2 shares of an 8 leaf tree",
			start:    3,
			end:      4,
			maxDepth: 3,
			expected: []coord{
				{
					depth:    3,
					position: 3,
				},
				{
					depth:    3,
					position: 4,
				},
			},
		},
		{
			name:     "third lone share of an 8 leaf tree",
			start:    3,
			end:      3,
			maxDepth: 3,
			expected: []coord{
				{
					depth:    3,
					position: 3,
				},
			},
		},
		{
			name:     "middle 3 shares of an 8 leaf tree",
			start:    3,
			end:      5,
			maxDepth: 3,
			expected: []coord{
				{
					depth:    3,
					position: 3,
				},
				{
					depth:    2,
					position: 2,
				},
			},
		},
		{
			name:     "middle 6 shares of an 8 leaf tree",
			start:    1,
			end:      6,
			maxDepth: 3,
			expected: []coord{
				{
					depth:    3,
					position: 1,
				},
				{
					depth:    2,
					position: 1,
				},
				{
					depth:    2,
					position: 2,
				},
				{
					depth:    3,
					position: 6,
				},
			},
		},
		{
			name:     "first 5 shares of an 8 leaf tree",
			start:    0,
			end:      4,
			maxDepth: 3,
			expected: []coord{
				{
					depth:    1,
					position: 0,
				},
				{
					depth:    3,
					position: 4,
				},
			},
		},
		{
			name:     "first 7 shares of an 8 leaf tree",
			start:    0,
			end:      6,
			maxDepth: 3,
			expected: []coord{
				{
					depth:    1,
					position: 0,
				},
				{
					depth:    2,
					position: 2,
				},
				{
					depth:    3,
					position: 6,
				},
			},
		},
		{
			name:     "all shares of an 8 leaf tree",
			start:    0,
			end:      7,
			maxDepth: 3,
			expected: []coord{
				{
					depth:    0,
					position: 0,
				},
			},
		},
		{
			name:     "first 32 shares of a 128 leaf tree",
			start:    0,
			end:      31,
			maxDepth: 7,
			expected: []coord{
				{
					depth:    2,
					position: 0,
				},
			},
		},
		{
			name:     "first 33 shares of a 128 leaf tree",
			start:    0,
			end:      32,
			maxDepth: 7,
			expected: []coord{
				{
					depth:    2,
					position: 0,
				},
				{
					depth:    7,
					position: 32,
				},
			},
		},
		{
			name:     "first 31 shares of a 128 leaf tree",
			start:    0,
			end:      30,
			maxDepth: 7,
			expected: []coord{
				{
					depth:    3,
					position: 0,
				},
				{
					depth:    4,
					position: 2,
				},
				{
					depth:    5,
					position: 6,
				},
				{
					depth:    6,
					position: 14,
				},
				{
					depth:    7,
					position: 30,
				},
			},
		},
		{
			name:     "first 64 shares of a 128 leaf tree",
			start:    0,
			end:      63,
			maxDepth: 7,
			expected: []coord{
				{
					depth:    1,
					position: 0,
				},
			},
		},
	}
	for _, tt := range tests {
		res := calculateSubTreeRootCoordinates(tt.maxDepth, tt.start, tt.end)
		assert.Equal(t, tt.expected, res, tt.name)
	}
}

func Test_genSubTreeRootPath(t *testing.T) {
	type test struct {
		depth    int
		pos      uint
		expected []bool
	}
	tests := []test{
		{2, 0, []bool{false, false}},
		{0, 0, []bool{}},
		{3, 0, []bool{false, false, false}},
		{3, 1, []bool{false, false, true}},
		{3, 2, []bool{false, true, false}},
		{5, 16, []bool{true, false, false, false, false}},
	}
	for _, tt := range tests {
		path := genSubTreeRootPath(tt.depth, tt.pos)
		assert.Equal(t, tt.expected, path)
	}
}
