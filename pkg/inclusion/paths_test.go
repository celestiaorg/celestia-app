package inclusion

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_calculateSubTreeRootCoordinates(t *testing.T) {
	type test struct {
		name     string
		start    int
		end      int
		maxDepth int
		minDepth int
		expected []coord
	}
	tests := []test{
		{
			name:     "first four shares of an 8 leaf tree",
			start:    0,
			end:      4,
			maxDepth: 3,
			minDepth: 1,
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
			end:      8,
			maxDepth: 3,
			minDepth: 1,
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
			end:      5,
			maxDepth: 3,
			minDepth: 3,
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
			end:      4,
			maxDepth: 3,
			minDepth: 3,
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
			end:      6,
			maxDepth: 3,
			minDepth: 2,
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
			end:      7,
			maxDepth: 3,
			minDepth: 2,
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
			name:     "middle 6 shares of an 8 leaf tree with minDepth 3",
			start:    1,
			end:      7,
			maxDepth: 3,
			minDepth: 3,
			expected: []coord{
				{
					depth:    3,
					position: 1,
				},
				{
					depth:    3,
					position: 2,
				},
				{
					depth:    3,
					position: 3,
				},
				{
					depth:    3,
					position: 4,
				},
				{
					depth:    3,
					position: 5,
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
			end:      5,
			maxDepth: 3,
			minDepth: 1,
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
			end:      7,
			maxDepth: 3,
			minDepth: 1,
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
			end:      8,
			maxDepth: 3,
			minDepth: 0,
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
			end:      32,
			maxDepth: 7,
			minDepth: 2,
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
			end:      33,
			maxDepth: 7,
			minDepth: 2,
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
			end:      31,
			maxDepth: 7,
			minDepth: 3,
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
			end:      64,
			maxDepth: 7,
			minDepth: 1,
			expected: []coord{
				{
					depth:    1,
					position: 0,
				},
			},
		},
		{
			name:     "single leaf square size 4",
			start:    0,
			end:      1,
			maxDepth: 2,
			minDepth: 2,
			expected: []coord{
				{
					depth:    2,
					position: 0,
				},
			},
		},
		{
			name:     "first 19 shares of a 64 x 64 square",
			start:    0,
			end:      19,
			maxDepth: 6, // implies a squareSize of 64 because log2(64) = 6
			minDepth: 3,
			expected: []coord{
				{
					depth:    3,
					position: 0,
				},
				{
					depth:    3,
					position: 1,
				},
				{
					depth:    5,
					position: 8,
				},
				{
					depth:    6,
					position: 18,
				},
			},
		},
	}
	for _, tt := range tests {
		res := calculateSubTreeRootCoordinates(tt.maxDepth, tt.minDepth, tt.start, tt.end)
		assert.Equal(t, tt.expected, res, tt.name)
	}
}
