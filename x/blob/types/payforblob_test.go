package types

import (
	"bytes"
	"testing"

	sdkerrors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func Test_merkleMountainRangeHeights(t *testing.T) {
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
		res := merkleMountainRangeSizes(tt.totalSize, tt.squareSize)
		assert.Equal(t, tt.expected, res)
	}
}

// TestCreateCommitment only shows if something changed, it doesn't actually
// show that the commitment bytes are being created correctly.
// TODO: verify the commitment bytes
func TestCreateCommitment(t *testing.T) {
	unsupportedShareVersion := uint8(1)

	type test struct {
		name         string
		namespace    []byte
		blob         []byte
		expected     []byte
		expectErr    bool
		shareVersion uint8
	}
	tests := []test{
		{
			name:         "blob of 11 shares succeeds",
			namespace:    bytes.Repeat([]byte{0xFF}, 8),
			blob:         bytes.Repeat([]byte{0xFF}, 11*ShareSize),
			expected:     []byte{0x9f, 0x44, 0xd5, 0x12, 0xe9, 0x6b, 0xea, 0xb3, 0xf2, 0xfe, 0x7b, 0x46, 0xc6, 0x4c, 0xee, 0x70, 0xb0, 0x86, 0xca, 0x94, 0x7e, 0x1b, 0x95, 0xd2, 0x0, 0x78, 0x32, 0xb5, 0x94, 0x68, 0x67, 0xf0},
			shareVersion: appconsts.ShareVersionZero,
		},
		{
			name:         "blob of 12 shares succeeds",
			namespace:    bytes.Repeat([]byte{0xFF}, 8),
			blob:         bytes.Repeat([]byte{0xFF}, 12*ShareSize),
			expected:     []byte{0xc0, 0x1a, 0xd7, 0xef, 0x37, 0x37, 0x9f, 0x62, 0x9c, 0x3a, 0x9, 0x9a, 0x5a, 0x1b, 0xff, 0xb7, 0x7a, 0xfa, 0xf6, 0x61, 0x19, 0x5b, 0x1a, 0xdb, 0x21, 0x84, 0x4, 0xac, 0x42, 0x7f, 0xec, 0xdf},
			shareVersion: appconsts.ShareVersionZero,
		},
		{
			name:         "blob with unsupported share version should return error",
			namespace:    bytes.Repeat([]byte{0xFF}, 8),
			blob:         bytes.Repeat([]byte{0xFF}, 12*ShareSize),
			expectErr:    true,
			shareVersion: unsupportedShareVersion,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := CreateCommitment(tt.namespace, tt.blob, tt.shareVersion)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, res)
		})
	}
}

func TestValidateBasic(t *testing.T) {
	type test struct {
		name    string
		msg     *MsgPayForBlob
		wantErr *sdkerrors.Error
	}

	validMsg := validMsgPayForBlob(t)

	// MsgPayForBlob that uses parity shares namespace id
	paritySharesMsg := validMsgPayForBlob(t)
	paritySharesMsg.NamespaceId = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	// MsgPayForBlob that uses tail padding namespace id
	tailPaddingMsg := validMsgPayForBlob(t)
	tailPaddingMsg.NamespaceId = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE}

	// MsgPayForBlob that uses transaction namespace id
	txNamespaceMsg := validMsgPayForBlob(t)
	txNamespaceMsg.NamespaceId = namespace.ID{0, 0, 0, 0, 0, 0, 0, 1}

	// MsgPayForBlob that uses intermediateStateRoots namespace id
	intermediateStateRootsNamespaceMsg := validMsgPayForBlob(t)
	intermediateStateRootsNamespaceMsg.NamespaceId = namespace.ID{0, 0, 0, 0, 0, 0, 0, 2}

	// MsgPayForBlob that uses evidence namespace id
	evidenceNamespaceMsg := validMsgPayForBlob(t)
	evidenceNamespaceMsg.NamespaceId = namespace.ID{0, 0, 0, 0, 0, 0, 0, 3}

	// MsgPayForBlob that uses the max reserved namespace id
	maxReservedNamespaceMsg := validMsgPayForBlob(t)
	maxReservedNamespaceMsg.NamespaceId = namespace.ID{0, 0, 0, 0, 0, 0, 0, 255}

	// MsgPayForBlob that has an empty share commitment
	emptyShareCommitment := validMsgPayForBlob(t)
	emptyShareCommitment.ShareCommitment = []byte{}

	tests := []test{
		{
			name:    "valid msg",
			msg:     validMsg,
			wantErr: nil,
		},
		{
			name:    "parity shares namespace id",
			msg:     paritySharesMsg,
			wantErr: ErrParitySharesNamespace,
		},
		{
			name:    "tail padding namespace id",
			msg:     tailPaddingMsg,
			wantErr: ErrTailPaddingNamespace,
		},
		{
			name:    "transaction namspace namespace id",
			msg:     txNamespaceMsg,
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "intermediate state root namespace id",
			msg:     intermediateStateRootsNamespaceMsg,
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "evidence namespace namespace id",
			msg:     evidenceNamespaceMsg,
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "max reserved namespace id",
			msg:     maxReservedNamespaceMsg,
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "empty share commitment",
			msg:     emptyShareCommitment,
			wantErr: ErrEmptyShareCommitment,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr != nil {
				assert.Contains(t, err.Error(), tt.wantErr.Error())
				space, code, log := sdkerrors.ABCIInfo(err, false)
				assert.Equal(t, tt.wantErr.Codespace(), space)
				assert.Equal(t, tt.wantErr.ABCICode(), code)
				t.Log(log)
			}
		})
	}
}

// totalBlobSize subtracts the delimiter size from the desired total size. this
// is useful for testing for blobs that occupy exactly so many shares.
func totalBlobSize(size int) int {
	return size - shares.DelimLen(uint64(size))
}

func validMsgPayForBlob(t *testing.T) *MsgPayForBlob {
	signer := GenerateKeyringSigner(t, TestAccName)
	ns := []byte{1, 1, 1, 1, 1, 1, 1, 2}
	blob := bytes.Repeat([]byte{2}, totalBlobSize(appconsts.ContinuationSparseShareContentSize*12))

	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)

	pfb, err := NewMsgPayForBlob(addr.String(), ns, blob)
	assert.NoError(t, err)

	return pfb
}

func TestNewMsgPayForBlob(t *testing.T) {
	type test struct {
		signer      string
		nids        [][]byte
		blobs       [][]byte
		versions    []uint8
		expectedErr bool
	}

	kr := GenerateKeyring(t, "blob")
	rec, err := kr.Key("blob")
	require.NoError(t, err)
	addr, err := rec.GetAddress()
	require.NoError(t, err)

	tests := []test{
		{
			signer:      addr.String(),
			nids:        [][]byte{{1, 2, 3, 4, 5, 6, 7, 8}},
			blobs:       [][]byte{{1}},
			versions:    make([]uint8, 1),
			expectedErr: false,
		},
		{
			signer:      addr.String(),
			nids:        [][]byte{{1, 2, 3, 4, 5, 6, 7, 8}},
			blobs:       [][]byte{tmrand.Bytes(1000000)},
			versions:    make([]uint8, 1),
			expectedErr: false,
		},
		{
			signer:      addr.String(),
			nids:        [][]byte{{1, 2, 3, 4, 5, 6, 7}},
			blobs:       [][]byte{tmrand.Bytes(100)},
			versions:    make([]uint8, 1),
			expectedErr: true,
		},
		{
			signer:      addr.String(),
			nids:        [][]byte{appconsts.TxNamespaceID},
			blobs:       [][]byte{tmrand.Bytes(100)},
			versions:    make([]uint8, 1),
			expectedErr: true,
		},
		{
			signer:      addr.String()[:10],
			nids:        [][]byte{{1, 2, 3, 4, 5, 6, 7, 8}},
			blobs:       [][]byte{tmrand.Bytes(100)},
			versions:    make([]uint8, 1),
			expectedErr: true,
		},
	}
	for _, tt := range tests {
		res, err := NewMsgPayForBlob(tt.signer, tt.nids[0], tt.blobs[0])
		if tt.expectedErr {
			assert.Error(t, err)
			continue
		}

		expectedCommitment, err := CreateMultiShareCommitment(tt.nids, tt.blobs, make([]uint32, len(tt.blobs)))
		require.NoError(t, err)
		assert.Equal(t, expectedCommitment, res.ShareCommitment)

		assert.Equal(t, uint32(len(tt.blobs[0])), res.BlobSize)
	}
}

func TestBlobMinSquareSize(t *testing.T) {
	type testCase struct {
		name     string
		blobSize uint64
		expected uint64
	}
	tests := []testCase{
		{
			name:     "1 byte",
			blobSize: 1,
			expected: 1,
		},
		{
			name:     "100 bytes",
			blobSize: 100,
			expected: 1,
		},
		{
			name:     "2 sparse shares",
			blobSize: appconsts.FirstCompactShareContentSize + appconsts.ContinuationCompactShareContentSize,
			expected: 2,
		},
		{
			name:     "5 sparse shares",
			blobSize: appconsts.FirstCompactShareContentSize + appconsts.ContinuationCompactShareContentSize*4,
			expected: 4,
		},
		{
			name:     "17 sparse shares",
			blobSize: appconsts.FirstCompactShareContentSize + appconsts.ContinuationCompactShareContentSize*16,
			expected: 8,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BlobMinSquareSize(tc.blobSize)
			assert.Equal(t, tc.expected, got)
		})
	}
}
