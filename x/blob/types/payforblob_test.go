package types

import (
	"bytes"
	"testing"

	sdkerrors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	testutilns "github.com/celestiaorg/celestia-app/testutil/namespace"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
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
		res, err := merkleMountainRangeSizes(tt.totalSize, tt.squareSize)
		require.NoError(t, err)
		assert.Equal(t, tt.expected, res)
	}
}

// TestCreateCommitment only shows if something changed, it doesn't actually
// show that the commitment bytes are being created correctly.
// TODO: verify the commitment bytes
func TestCreateCommitment(t *testing.T) {
	unsupportedShareVersion := uint8(1)
	namespaceOne := bytes.Repeat([]byte{1}, appconsts.NamespaceSize)

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
			namespace:    namespaceOne,
			blob:         bytes.Repeat([]byte{0xFF}, 11*ShareSize),
			expected:     []byte{0x5a, 0x26, 0x43, 0xa1, 0x54, 0xbe, 0x4b, 0xcc, 0x71, 0x95, 0xa1, 0xaa, 0xc2, 0x3f, 0xe9, 0x75, 0xed, 0x30, 0x32, 0xe5, 0x47, 0xad, 0xea, 0xdd, 0x39, 0xcd, 0xaa, 0xe5, 0xf8, 0x8d, 0x2e, 0x97},
			shareVersion: appconsts.ShareVersionZero,
		},
		{
			name:         "blob of 12 shares succeeds",
			namespace:    namespaceOne,
			blob:         bytes.Repeat([]byte{0xFF}, 12*ShareSize),
			expected:     []byte{0x49, 0x79, 0xf7, 0x3a, 0x2, 0x92, 0x2b, 0x57, 0xa8, 0x93, 0xc0, 0x98, 0xea, 0x87, 0x6b, 0x29, 0xaf, 0xd3, 0x11, 0x6, 0xd2, 0x4e, 0x50, 0x41, 0x1b, 0x4b, 0xad, 0xe4, 0x52, 0x41, 0x75, 0x26},
			shareVersion: appconsts.ShareVersionZero,
		},
		{
			name:         "blob with unsupported share version should return error",
			namespace:    namespaceOne,
			blob:         bytes.Repeat([]byte{0xFF}, 12*ShareSize),
			expectErr:    true,
			shareVersion: unsupportedShareVersion,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob := &Blob{NamespaceId: tt.namespace, Data: tt.blob, ShareVersion: uint32(tt.shareVersion)}
			res, err := CreateCommitment(blob)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, res)
		})
	}
}

func TestMsgTypeURLParity(t *testing.T) {
	require.Equal(t, sdk.MsgTypeURL(&MsgPayForBlobs{}), URLMsgPayForBlobs)
}

func TestValidateBasic(t *testing.T) {
	type test struct {
		name    string
		msg     *MsgPayForBlobs
		wantErr *sdkerrors.Error
	}

	validMsg := validMsgPayForBlobs(t)

	// MsgPayForBlobs that uses parity shares namespace id
	paritySharesMsg := validMsgPayForBlobs(t)
	paritySharesMsg.NamespaceIds[0] = appconsts.ParitySharesNamespaceID

	// MsgPayForBlobs that uses tail padding namespace id
	tailPaddingMsg := validMsgPayForBlobs(t)
	tailPaddingMsg.NamespaceIds[0] = appconsts.TailPaddingNamespaceID

	// MsgPayForBlobs that uses transaction namespace id
	txNamespaceMsg := validMsgPayForBlobs(t)
	txNamespaceMsg.NamespaceIds[0] = appconsts.TxNamespaceID

	// MsgPayForBlobs that uses intermediateStateRoots namespace id
	intermediateStateRootsNamespaceMsg := validMsgPayForBlobs(t)
	intermediateStateRootsNamespaceMsg.NamespaceIds[0] = appconsts.IntermediateStateRootsNamespaceID

	// MsgPayForBlobs that uses evidence namespace id
	evidenceNamespaceMsg := validMsgPayForBlobs(t)
	evidenceNamespaceMsg.NamespaceIds[0] = appconsts.EvidenceNamespaceID

	// MsgPayForBlobs that uses the max reserved namespace id
	maxReservedNamespaceMsg := validMsgPayForBlobs(t)
	maxReservedNamespaceMsg.NamespaceIds[0] = appconsts.MaxReservedNamespace

	// MsgPayForBlobs that has an empty share commitment
	emptyShareCommitment := validMsgPayForBlobs(t)
	emptyShareCommitment.ShareCommitments[0] = []byte{}

	// MsgPayForBlobs that has no namespace ids
	noNamespaceIds := validMsgPayForBlobs(t)
	noNamespaceIds.NamespaceIds = [][]byte{}

	// MsgPayForBlobs that has no share versions
	noShareVersions := validMsgPayForBlobs(t)
	noShareVersions.ShareVersions = []uint32{}

	// MsgPayForBlobs that has no blob sizes
	noBlobSizes := validMsgPayForBlobs(t)
	noBlobSizes.BlobSizes = []uint32{}

	// MsgPayForBlobs that has no share commitments
	noShareCommitments := validMsgPayForBlobs(t)
	noShareCommitments.ShareCommitments = [][]byte{}

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
		{
			name:    "no namespace ids",
			msg:     noNamespaceIds,
			wantErr: ErrNoNamespaceIds,
		},
		{
			name:    "no share versions",
			msg:     noShareVersions,
			wantErr: ErrNoShareVersions,
		},
		{
			name:    "no blob sizes",
			msg:     noBlobSizes,
			wantErr: ErrNoBlobSizes,
		},
		{
			name:    "no share commitments",
			msg:     noShareCommitments,
			wantErr: ErrNoShareCommitments,
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

func validMsgPayForBlobs(t *testing.T) *MsgPayForBlobs {
	signer := GenerateKeyringSigner(t, TestAccName)
	ns := bytes.Repeat([]byte{1}, appconsts.NamespaceSize)
	blob := bytes.Repeat([]byte{2}, totalBlobSize(appconsts.ContinuationSparseShareContentSize*12))

	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)

	pblob := &tmproto.Blob{
		Data:         blob,
		NamespaceId:  ns,
		ShareVersion: uint32(appconsts.ShareVersionZero),
	}

	pfb, err := NewMsgPayForBlobs(addr.String(), pblob)
	assert.NoError(t, err)

	return pfb
}

func TestNewMsgPayForBlobs(t *testing.T) {
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
			nids:        [][]byte{testutilns.RandomBlobNamespace()},
			blobs:       [][]byte{{1}},
			versions:    make([]uint8, 1),
			expectedErr: false,
		},
		{
			signer:      addr.String(),
			nids:        [][]byte{testutilns.RandomBlobNamespace()},
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
			nids:        [][]byte{testutilns.RandomBlobNamespace()},
			blobs:       [][]byte{tmrand.Bytes(100)},
			versions:    make([]uint8, 1),
			expectedErr: true,
		},
	}
	for _, tt := range tests {
		blob := &Blob{NamespaceId: tt.nids[0], Data: tt.blobs[0], ShareVersion: uint32(appconsts.DefaultShareVersion)}
		mpfb, err := NewMsgPayForBlobs(tt.signer, blob)
		if tt.expectedErr {
			assert.Error(t, err)
			continue
		}

		expectedCommitment, err := CreateCommitment(blob)
		require.NoError(t, err)
		assert.Equal(t, expectedCommitment, mpfb.ShareCommitments[0])
		assert.Equal(t, uint32(len(tt.blobs[0])), mpfb.BlobSizes[0])
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

func TestValidateBlobs(t *testing.T) {
	type test struct {
		name        string
		blob        *Blob
		expectError bool
	}

	tests := []test{
		{name: "valid blob", blob: &Blob{Data: []byte{1}, NamespaceId: testutilns.RandomBlobNamespace(), ShareVersion: uint32(appconsts.DefaultShareVersion)}, expectError: false},
		{name: "invalid share version", blob: &Blob{Data: []byte{1}, NamespaceId: testutilns.RandomBlobNamespace(), ShareVersion: uint32(10000)}, expectError: true},
		{name: "empty blob", blob: &Blob{Data: []byte{}, NamespaceId: testutilns.RandomBlobNamespace(), ShareVersion: uint32(appconsts.DefaultShareVersion)}, expectError: true},
		{name: "invalid namespace", blob: &Blob{Data: []byte{1}, NamespaceId: appconsts.TxNamespaceID, ShareVersion: uint32(appconsts.DefaultShareVersion)}, expectError: true},
	}

	for _, tt := range tests {
		err := ValidateBlobs(tt.blob)
		if tt.expectError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}
