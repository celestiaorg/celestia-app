package types

import (
	"bytes"
	"testing"

	sdkerrors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt/namespace"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			expected:     []byte{0x1e, 0xdc, 0xc4, 0x69, 0x8f, 0x47, 0xf6, 0x8d, 0xfc, 0x11, 0xec, 0xac, 0xaa, 0x37, 0x4a, 0x3d, 0xbd, 0xfc, 0x1a, 0x9b, 0x6e, 0x87, 0x6f, 0xba, 0xd3, 0x6c, 0x6, 0x6c, 0x9f, 0x5b, 0x65, 0x38},
			shareVersion: appconsts.ShareVersionZero,
		},
		{
			name:         "blob of 12 shares succeeds",
			namespace:    bytes.Repeat([]byte{0xFF}, 8),
			blob:         bytes.Repeat([]byte{0xFF}, 12*ShareSize),
			expected:     []byte{0x81, 0x5e, 0xf9, 0x52, 0x2a, 0xfa, 0x40, 0x67, 0x63, 0x64, 0x4a, 0x82, 0x7, 0xcd, 0x1d, 0x7d, 0x1f, 0xae, 0xe5, 0xd3, 0xb1, 0x91, 0x8a, 0xb8, 0x90, 0x51, 0xfc, 0x1, 0xd, 0xa7, 0xf3, 0x1a},
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

// TestSignMalleatedTxs checks to see that the signatures that are generated for
// the PayForBlobs malleated from the original WirePayForBlob are actually
// valid.
func TestSignMalleatedTxs(t *testing.T) {
	type test struct {
		name     string
		ns       []byte
		blobData []byte
		options  []TxBuilderOption
	}

	signer := GenerateKeyringSigner(t, TestAccName)

	tests := []test{
		{
			name:     "single share",
			ns:       []byte{1, 1, 1, 1, 1, 1, 1, 1},
			blobData: bytes.Repeat([]byte{1}, appconsts.SparseShareContentSize),
			options:  []TxBuilderOption{SetGasLimit(2000000)},
		},
		{
			name:     "12 shares",
			ns:       []byte{1, 1, 1, 1, 1, 1, 1, 2},
			blobData: bytes.Repeat([]byte{2}, (appconsts.SparseShareContentSize*12)-4), // subtract a few bytes for the delimiter
			options: []TxBuilderOption{
				SetGasLimit(123456789),
				SetFeeAmount(sdk.NewCoins(sdk.NewCoin("utia", sdk.NewInt(987654321)))),
			},
		},
		{
			name:     "12 shares",
			ns:       []byte{1, 1, 1, 1, 1, 1, 1, 2},
			blobData: bytes.Repeat([]byte{1, 2, 3, 4, 5}, 10000), // subtract a few bytes for the delimiter
			options: []TxBuilderOption{
				SetGasLimit(123456789),
				SetFeeAmount(sdk.NewCoins(sdk.NewCoin("utia", sdk.NewInt(987654321)))),
			},
		},
	}

	for _, tt := range tests {
		wpfb, err := NewWirePayForBlob(tt.ns, tt.blobData, appconsts.ShareVersionZero)
		require.NoError(t, err, tt.name)
		err = wpfb.SignShareCommitment(signer, tt.options...)
		// there should be no error
		assert.NoError(t, err)
		// the signature should exist
		assert.Equal(t, len(wpfb.ShareCommitment.Signature), 64)

		sData, err := signer.GetSignerData()
		require.NoError(t, err)

		wpfbTx, err := signer.BuildSignedTx(signer.NewTxBuilder(tt.options...), wpfb)
		require.NoError(t, err)

		// VerifyPFBSigs goes through the entire malleation process for every
		// square size, creating PfBs from the wirePfB and check that the
		// signature is valid
		valid, err := VerifyPFBSigs(sData, signer.encCfg.TxConfig, wpfbTx)
		assert.NoError(t, err)
		assert.True(t, valid, tt.name)
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
			name:    "evidence namspace namespace id",
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

func validWirePayForBlob(t *testing.T) *MsgWirePayForBlob {
	blob := bytes.Repeat([]byte{1}, 2000)
	msgWPFB, err := NewWirePayForBlob(
		[]byte{1, 2, 3, 4, 5, 6, 7, 8},
		blob,
		appconsts.ShareVersionZero,
	)
	if err != nil {
		panic(err)
	}

	signer := GenerateKeyringSigner(t)

	err = msgWPFB.SignShareCommitment(signer)
	if err != nil {
		panic(err)
	}
	return msgWPFB
}

func validMsgPayForBlob(t *testing.T) *MsgPayForBlob {
	signer := GenerateKeyringSigner(t, TestAccName)
	ns := []byte{1, 1, 1, 1, 1, 1, 1, 2}
	blob := bytes.Repeat([]byte{2}, totalBlobSize(appconsts.SparseShareContentSize*12))

	wpfb, err := NewWirePayForBlob(ns, blob, appconsts.ShareVersionZero)
	assert.NoError(t, err)

	err = wpfb.SignShareCommitment(signer)
	assert.NoError(t, err)

	_, spfb, _, err := ProcessWireMsgPayForBlob(wpfb)
	require.NoError(t, err)

	return spfb
}
