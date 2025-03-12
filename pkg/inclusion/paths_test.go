package inclusion

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
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
		name                string
		squareSize          int
		start               int
		blobLen             int
		expectedPath        []path
		expectedPathIndexes []int
	}
	// note that calculateCommitPaths assumes ODS, so we prepend a WalkLeft to
	// everything elsewhere
	tests := []test{
		{
			"all paths for a basic 2x2", 2, 2, 2,
			[]path{
				{
					row:          1,
					instructions: []WalkInstruction{WalkLeft},
				},
				{
					row:          1,
					instructions: []WalkInstruction{WalkRight},
				},
			},
			[]int{0, 1},
		},
		{
			"all paths for a basic 4x4", 4, 2, 2,
			[]path{
				{
					row:          0,
					instructions: []WalkInstruction{WalkRight, WalkLeft},
				},
				{
					row:          0,
					instructions: []WalkInstruction{WalkRight, WalkRight},
				},
			},
			[]int{0, 1},
		},
		{
			"all paths for a basic 4x4 span more than 1 row", 4, 3, 2,
			[]path{
				{
					row:          0,
					instructions: []WalkInstruction{WalkRight, WalkRight},
				},
				{
					row:          1,
					instructions: []WalkInstruction{WalkLeft, WalkLeft},
				},
			},
			[]int{0, 1},
		},
		{
			"single share in the middle of a 128x128", 128, 8252, 1,
			[]path{
				{
					row:          64,
					instructions: []WalkInstruction{WalkLeft, WalkRight, WalkRight, WalkRight, WalkRight, WalkLeft, WalkLeft},
				},
			},
			[]int{0},
		},
		{
			"the 32nd path for the smallest blob with a subtree width of 128", 128, 0, 8193,
			[]path{
				{
					row:          31,
					instructions: []WalkInstruction{}, // there should be no instructions because we're using the first root
				},
			},
			[]int{31},
		},
		{
			"the 32nd path for the largest blob with a subtree width of 64", 128, 0, 8192,
			[]path{
				{
					row:          31,
					instructions: []WalkInstruction{},
				},
			},
			[]int{31},
		},
		{
			"the 32nd path for the largest blob with a subtree width of 1", 128, 0, appconsts.DefaultSubtreeRootThreshold,
			[]path{
				{
					row:          0,
					instructions: []WalkInstruction{WalkLeft, WalkLeft, WalkRight, WalkRight, WalkRight, WalkRight, WalkRight},
				},
			},
			[]int{31},
		},
		{
			"the 32nd and last path for the smallest blob with a subtree width of 2", 128, 0, appconsts.DefaultSubtreeRootThreshold + 1,
			[]path{
				{
					row:          0,
					instructions: []WalkInstruction{WalkLeft, WalkRight, WalkRight, WalkRight, WalkRight, WalkRight},
				},
				// note that the last path should be one instruction longer
				{
					row:          0,
					instructions: []WalkInstruction{WalkRight, WalkLeft, WalkLeft, WalkLeft, WalkLeft, WalkLeft, WalkLeft},
				},
			},
			[]int{31, 32},
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				paths := calculateCommitmentPaths(tt.squareSize, tt.start, tt.blobLen, appconsts.DefaultSubtreeRootThreshold)
				for j, pi := range tt.expectedPathIndexes {
					assert.Equal(t, tt.expectedPath[j], paths[pi])
				}
				// check that each path is unique
				pm := make(map[string]struct{})
				for _, p := range paths {
					sp := pathToString(p)
					_, has := pm[sp]
					require.False(t, has)
					pm[sp] = struct{}{}
				}
			},
		)
	}
}

func pathToString(p path) string {
	s := fmt.Sprintf("%d", p.row)
	for _, wi := range p.instructions {
		switch wi {
		case WalkLeft:
			s += "l"
		case WalkRight:
			s += "r"
		}
	}
	return s
}
