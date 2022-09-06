package inclusion

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_calculateSubTreeRootCoordinates(t *testing.T) {
	type test struct {
		name                 string
		start, end, maxDepth int
		expected             []coord
	}
	tests := []test{
		{
			name:     "first four shares of an 8 leaf tree",
			start:    0,
			end:      4,
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
			end:      8,
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
			end:      5,
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
			end:      4,
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
			end:      6,
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
			end:      7,
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
			end:      5,
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
			end:      7,
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
			end:      8,
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
			end:      32,
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
			end:      33,
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
			end:      31,
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
			end:      64,
			maxDepth: 7,
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
			expected: []coord{
				{
					depth:    2,
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
		expected []WalkInstruction
	}
	tests := []test{
		{2, 0, []WalkInstruction{WalkLeft, WalkLeft}},
		{0, 0, []WalkInstruction{}},
		{3, 0, []WalkInstruction{WalkLeft, WalkLeft, WalkLeft}},
		{3, 1, []WalkInstruction{WalkLeft, WalkLeft, WalkRight}},
		{3, 2, []WalkInstruction{WalkLeft, WalkRight, WalkLeft}},
		{5, 16, []WalkInstruction{WalkRight, WalkLeft, WalkLeft, WalkLeft, WalkLeft}},
	}
	for _, tt := range tests {
		path := genSubTreeRootPath(tt.depth, tt.pos)
		assert.Equal(t, tt.expected, path)
	}
}

func Test_calculateCommitPaths(t *testing.T) {
	type test struct {
		size, start, msgLen int
		expected            []path
	}
	tests := []test{
		{2, 0, 1, []path{{instructions: []WalkInstruction{WalkLeft}, row: 0}}},
		{2, 2, 2, []path{{instructions: []WalkInstruction{}, row: 1}}},
		{2, 1, 2, []path{{instructions: []WalkInstruction{}, row: 1}}},
		{4, 2, 2, []path{{instructions: []WalkInstruction{WalkRight}, row: 0}}},
		{4, 2, 4, []path{{instructions: []WalkInstruction{}, row: 1}}},
		{4, 3, 4, []path{{instructions: []WalkInstruction{}, row: 1}}},
		{4, 2, 9, []path{
			{instructions: []WalkInstruction{}, row: 1},
			{instructions: []WalkInstruction{}, row: 2},
			{instructions: []WalkInstruction{WalkLeft, WalkLeft}, row: 3},
		}},
		{8, 3, 16, []path{
			{instructions: []WalkInstruction{}, row: 1},
			{instructions: []WalkInstruction{}, row: 2},
		}},
		{64, 144, 32, []path{
			{instructions: []WalkInstruction{WalkRight}, row: 2},
		}},
		{64, 4032, 33, []path{
			{instructions: []WalkInstruction{WalkLeft}, row: 63},
			{instructions: []WalkInstruction{WalkRight, WalkLeft, WalkLeft, WalkLeft, WalkLeft, WalkLeft}, row: 63},
		}},
		{64, 4032, 63, []path{
			{instructions: []WalkInstruction{WalkLeft}, row: 63},
			{instructions: []WalkInstruction{WalkRight, WalkLeft}, row: 63},
			{instructions: []WalkInstruction{WalkRight, WalkRight, WalkLeft}, row: 63},
			{instructions: []WalkInstruction{WalkRight, WalkRight, WalkRight, WalkLeft}, row: 63},
			{instructions: []WalkInstruction{WalkRight, WalkRight, WalkRight, WalkRight, WalkLeft}, row: 63},
			{instructions: []WalkInstruction{WalkRight, WalkRight, WalkRight, WalkRight, WalkRight, WalkLeft}, row: 63},
		}},
	}
	for i, tt := range tests {
		t.Run(
			fmt.Sprintf("test %d: square size %d start %d msgLen %d", i, tt.size, tt.start, tt.msgLen),
			func(t *testing.T) {
				assert.Equal(t, tt.expected, calculateCommitPaths(tt.size, tt.start, tt.msgLen))
			},
		)
	}
}
