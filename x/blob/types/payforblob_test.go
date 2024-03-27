package types

import (
	"bytes"
	"testing"

	sdkerrors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
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
			blob:         bytes.Repeat([]byte{0xFF}, 3*ShareSize),
			expected:     []byte{0x3b, 0x9e, 0x78, 0xb6, 0x64, 0x8e, 0xc1, 0xa2, 0x41, 0x92, 0x5b, 0x31, 0xda, 0x2e, 0xcb, 0x50, 0xbf, 0xc6, 0xf4, 0xad, 0x55, 0x2d, 0x32, 0x79, 0x92, 0x8c, 0xa1, 0x3e, 0xbe, 0xba, 0x8c, 0x2b},
			shareVersion: appconsts.ShareVersionZero,
		},
		{
			name:         "blob with unsupported share version should return error",
			namespace:    ns1,
			blob:         bytes.Repeat([]byte{0xFF}, 12*ShareSize),
			expectErr:    true,
			shareVersion: uint8(1), // unsupported share version
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob := &Blob{
				NamespaceId:      tt.namespace.ID,
				Data:             tt.blob,
				ShareVersion:     uint32(tt.shareVersion),
				NamespaceVersion: uint32(tt.namespace.Version),
			}
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

	// MsgPayForBlobs that uses parity shares namespace
	paritySharesMsg := validMsgPayForBlobs(t)
	paritySharesMsg.Namespaces[0] = appns.ParitySharesNamespace.Bytes()

	// MsgPayForBlobs that uses tail padding namespace
	tailPaddingMsg := validMsgPayForBlobs(t)
	tailPaddingMsg.Namespaces[0] = appns.TailPaddingNamespace.Bytes()

	// MsgPayForBlobs that uses transaction namespace
	txNamespaceMsg := validMsgPayForBlobs(t)
	txNamespaceMsg.Namespaces[0] = appns.TxNamespace.Bytes()

	// MsgPayForBlobs that uses intermediateStateRoots namespace
	intermediateStateRootsNamespaceMsg := validMsgPayForBlobs(t)
	intermediateStateRootsNamespaceMsg.Namespaces[0] = appns.IntermediateStateRootsNamespace.Bytes()

	// MsgPayForBlobs that uses the max primary reserved namespace
	maxReservedNamespaceMsg := validMsgPayForBlobs(t)
	maxReservedNamespaceMsg.Namespaces[0] = appns.MaxPrimaryReservedNamespace.Bytes()

	// MsgPayForBlobs that has an empty share commitment
	emptyShareCommitment := validMsgPayForBlobs(t)
	emptyShareCommitment.ShareCommitments[0] = []byte{}

	// MsgPayForBlobs that has an invalid share commitment size
	invalidShareCommitmentSize := validMsgPayForBlobs(t)
	invalidShareCommitmentSize.ShareCommitments[0] = bytes.Repeat([]byte{0x1}, 31)

	// MsgPayForBlobs that has no namespaces
	noNamespaces := validMsgPayForBlobs(t)
	noNamespaces.Namespaces = [][]byte{}

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
			name:    "parity shares namespace",
			msg:     paritySharesMsg,
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "tail padding namespace",
			msg:     tailPaddingMsg,
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "tx namespace",
			msg:     txNamespaceMsg,
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "intermediate state root namespace",
			msg:     intermediateStateRootsNamespaceMsg,
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "max reserved namespace",
			msg:     maxReservedNamespaceMsg,
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "empty share commitment",
			msg:     emptyShareCommitment,
			wantErr: ErrInvalidShareCommitment,
		},
		{
			name:    "incorrect hash size share commitment",
			msg:     invalidShareCommitmentSize,
			wantErr: ErrInvalidShareCommitment,
		},
		{
			name:    "no namespace ids",
			msg:     noNamespaces,
			wantErr: ErrNoNamespaces,
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
		{
			name:    "invalid namespace version",
			msg:     invalidNamespaceVersionMsgPayForBlobs(t),
			wantErr: ErrInvalidNamespaceVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr != nil {
				require.Error(t, err)
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
	ns1 := append(appns.NamespaceVersionZeroPrefix, bytes.Repeat([]byte{0x01}, appns.NamespaceVersionZeroIDSize)...)
	blob := bytes.Repeat([]byte{2}, totalBlobSize(appconsts.ContinuationSparseShareContentSize*12))

	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)

	pblob := &tmproto.Blob{
		Data:             blob,
		NamespaceId:      ns1,
		NamespaceVersion: uint32(appns.NamespaceVersionZero),
		ShareVersion:     uint32(appconsts.ShareVersionZero),
	}

	pfb, err := NewMsgPayForBlobs(addr.String(), pblob)
	assert.NoError(t, err)

	return pfb
}

func invalidNamespaceVersionMsgPayForBlobs(t *testing.T) *MsgPayForBlobs {
	signer := GenerateKeyringSigner(t, TestAccName)
	ns1 := append(appns.NamespaceVersionZeroPrefix, bytes.Repeat([]byte{0x01}, appns.NamespaceVersionZeroIDSize)...)
	blob := bytes.Repeat([]byte{2}, totalBlobSize(appconsts.ContinuationSparseShareContentSize*12))

	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)

	pblob := &tmproto.Blob{
		Data:             blob,
		NamespaceId:      ns1,
		NamespaceVersion: uint32(255),
		ShareVersion:     uint32(appconsts.ShareVersionZero),
	}

	blobs := []*Blob{pblob}

	commitments, err := CreateCommitments(blobs)
	require.NoError(t, err)

	namespaceVersions, namespaceIDs, sizes, shareVersions := extractBlobComponents(blobs)
	namespaces := []appns.Namespace{}
	for i := range namespaceVersions {
		namespace, err := appns.New(uint8(namespaceVersions[i]), namespaceIDs[i])
		require.NoError(t, err)
		namespaces = append(namespaces, namespace)
	}

	pfb := &MsgPayForBlobs{
		Signer:           addr.String(),
		Namespaces:       namespacesToBytes(namespaces),
		ShareCommitments: commitments,
		BlobSizes:        sizes,
		ShareVersions:    shareVersions,
	}

	return pfb
}

func TestNewMsgPayForBlobs(t *testing.T) {
	type testCase struct {
		name        string
		signer      string
		blobs       []*tmproto.Blob
		expectedErr bool
	}

	kr := GenerateKeyring(t, "blob")
	rec, err := kr.Key("blob")
	require.NoError(t, err)
	addr, err := rec.GetAddress()
	require.NoError(t, err)
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	ns2 := appns.MustNewV0(bytes.Repeat([]byte{2}, appns.NamespaceVersionZeroIDSize))

	testCases := []testCase{
		{
			name:   "valid msg PFB with small blob",
			signer: addr.String(),
			blobs: []*tmproto.Blob{
				{
					NamespaceVersion: uint32(ns1.Version),
					NamespaceId:      ns1.ID,
					Data:             []byte{1},
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
			},
			expectedErr: false,
		},
		{
			name:   "valid msg PFB with large blob",
			signer: addr.String(),
			blobs: []*tmproto.Blob{
				{
					NamespaceVersion: uint32(ns1.Version),
					NamespaceId:      ns1.ID,
					Data:             tmrand.Bytes(1000000),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
			},
			expectedErr: false,
		},
		{
			name:   "valid msg PFB with two blobs",
			signer: addr.String(),
			blobs: []*tmproto.Blob{
				{
					NamespaceVersion: uint32(ns1.Version),
					NamespaceId:      ns1.ID,
					Data:             []byte{1},
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
				{
					NamespaceVersion: uint32(ns2.Version),
					NamespaceId:      ns2.ID,
					Data:             []byte{2},
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
			},
		},
		{
			name:   "unsupported share version returns an error",
			signer: addr.String(),
			blobs: []*tmproto.Blob{
				{
					NamespaceVersion: uint32(ns1.Version),
					NamespaceId:      ns1.ID,
					Data:             tmrand.Bytes(1000000),
					ShareVersion:     uint32(10), // unsupported share version
				},
			},
			expectedErr: true,
		},
		{
			name:   "msg PFB with tx namespace returns an error",
			signer: addr.String(),
			blobs: []*tmproto.Blob{
				{
					NamespaceVersion: uint32(appns.TxNamespace.Version),
					NamespaceId:      appns.TxNamespace.ID,
					Data:             tmrand.Bytes(1000000),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
			},
			expectedErr: true,
		},
		{
			name:   "msg PFB with invalid signer returns an error",
			signer: addr.String()[:10],
			blobs: []*tmproto.Blob{
				{
					NamespaceVersion: uint32(ns1.Version),
					NamespaceId:      ns1.ID,
					Data:             []byte{1},
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
			},
			expectedErr: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msgPFB, err := NewMsgPayForBlobs(tc.signer, tc.blobs...)
			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			for i, blob := range tc.blobs {
				assert.Equal(t, uint32(len(blob.Data)), msgPFB.BlobSizes[i])
				ns, err := appns.From(msgPFB.Namespaces[i])
				assert.NoError(t, err)
				assert.Equal(t, ns.ID, blob.NamespaceId)
				assert.Equal(t, uint32(ns.Version), blob.NamespaceVersion)

				expectedCommitment, err := CreateCommitment(blob)
				require.NoError(t, err)
				assert.Equal(t, expectedCommitment, msgPFB.ShareCommitments[i])
			}
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
		{
			name: "valid blob",
			blob: &Blob{
				Data:             []byte{1},
				NamespaceId:      appns.RandomBlobNamespace().ID,
				ShareVersion:     uint32(appconsts.DefaultShareVersion),
				NamespaceVersion: uint32(appns.NamespaceVersionZero),
			},
			expectError: false,
		},
		{
			name: "invalid share version",
			blob: &Blob{
				Data:             []byte{1},
				NamespaceId:      appns.RandomBlobNamespace().ID,
				ShareVersion:     uint32(10000),
				NamespaceVersion: uint32(appns.NamespaceVersionZero),
			},
			expectError: true,
		},
		{
			name: "empty blob",
			blob: &Blob{
				Data:             []byte{},
				NamespaceId:      appns.RandomBlobNamespace().ID,
				ShareVersion:     uint32(appconsts.DefaultShareVersion),
				NamespaceVersion: uint32(appns.NamespaceVersionZero),
			},
			expectError: true,
		},
		{
			name: "invalid namespace",
			blob: &Blob{
				Data:             []byte{1},
				NamespaceId:      appns.TxNamespace.ID,
				ShareVersion:     uint32(appconsts.DefaultShareVersion),
				NamespaceVersion: uint32(appns.NamespaceVersionZero),
			},
			expectError: true,
		},
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
