package inclusion_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/blob"
	"github.com/celestiaorg/celestia-app/pkg/inclusion"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_MerkleMountainRangeHeights(t *testing.T) {
	type test struct {
		totalSize  uint64
		squareSize uint64
		expected   []uint64
	}
	tests := []test{
		{
			totalSize:  11,
			squareSize: 4,
			expected:   []uint64{4, 4, 2, 1},
		},
		{
			totalSize:  2,
			squareSize: 64,
			expected:   []uint64{2},
		},
		{
			totalSize:  64,
			squareSize: 8,
			expected:   []uint64{8, 8, 8, 8, 8, 8, 8, 8},
		},
		// Height
		// 3              x                               x
		//              /    \                         /    \
		//             /      \                       /      \
		//            /        \                     /        \
		//           /          \                   /          \
		// 2        x            x                 x            x
		//        /   \        /   \             /   \        /   \
		// 1     x     x      x     x           x     x      x     x         x
		//      / \   / \    / \   / \         / \   / \    / \   / \      /   \
		// 0   0   1 2   3  4   5 6   7       8   9 10  11 12 13 14  15   16   17    18
		{
			totalSize:  19,
			squareSize: 8,
			expected:   []uint64{8, 8, 2, 1},
		},
	}
	for _, tt := range tests {
		res, err := inclusion.MerkleMountainRangeSizes(tt.totalSize, tt.squareSize)
		require.NoError(t, err)
		assert.Equal(t, tt.expected, res)
	}
}

// TestCreateCommitment will fail if a change is made to share encoding or how
// the commitment is calculated. If this is the case, the expected commitment
// bytes will need to be updated.
func TestCreateCommitment(t *testing.T) {
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{0x1}, appns.NamespaceVersionZeroIDSize))

	type test struct {
		name         string
		namespace    appns.Namespace
		blob         []byte
		expected     []byte
		expectErr    bool
		shareVersion uint8
	}
	tests := []test{
		{
			name:         "blob of 3 shares succeeds",
			namespace:    ns1,
			blob:         bytes.Repeat([]byte{0xFF}, 3*appconsts.ShareSize),
			expected:     []byte{0x3b, 0x9e, 0x78, 0xb6, 0x64, 0x8e, 0xc1, 0xa2, 0x41, 0x92, 0x5b, 0x31, 0xda, 0x2e, 0xcb, 0x50, 0xbf, 0xc6, 0xf4, 0xad, 0x55, 0x2d, 0x32, 0x79, 0x92, 0x8c, 0xa1, 0x3e, 0xbe, 0xba, 0x8c, 0x2b},
			shareVersion: appconsts.ShareVersionZero,
		},
		{
			name:         "blob with unsupported share version should return error",
			namespace:    ns1,
			blob:         bytes.Repeat([]byte{0xFF}, 12*appconsts.ShareSize),
			expectErr:    true,
			shareVersion: uint8(1), // unsupported share version
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob := &blob.Blob{
				NamespaceId:      tt.namespace.ID,
				Data:             tt.blob,
				ShareVersion:     uint32(tt.shareVersion),
				NamespaceVersion: uint32(tt.namespace.Version),
			}
			res, err := inclusion.CreateCommitment(blob)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, res)
		})
	}
}
