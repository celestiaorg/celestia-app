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
	type test struct {
		name          string
		minSquareSize int
		namespace     []byte
		message       []byte
		expected      []byte
		expectErr     bool
	}
	tests := []test{
		{
			name:          "squareSize 8, message of 11 shares succeeds",
			minSquareSize: 8,
			namespace:     bytes.Repeat([]byte{0xFF}, 8),
			message:       bytes.Repeat([]byte{0xFF}, 11*ShareSize),
			expected:      []byte{0xbf, 0xbd, 0x40, 0x5c, 0xc6, 0xb8, 0xe9, 0x21, 0x32, 0x52, 0xf6, 0x1, 0x22, 0x46, 0x8f, 0x24, 0x9d, 0x7f, 0x73, 0xac, 0xf, 0xaa, 0x29, 0x38, 0xdb, 0x81, 0xeb, 0x3d, 0x75, 0x3b, 0xed, 0x26},
		},
		{
			name:          "squareSize 8, message of 50 shares succeeds",
			minSquareSize: 8,
			namespace:     bytes.Repeat([]byte{0xFF}, 8),
			message:       bytes.Repeat([]byte{0xFF}, 50*ShareSize),
			expected:      []byte{0xe, 0x66, 0xc2, 0x2, 0x89, 0xa4, 0x57, 0xef, 0xee, 0x26, 0xd9, 0x35, 0x2f, 0x12, 0xe6, 0xfc, 0x30, 0x90, 0x2c, 0x4b, 0xab, 0xb6, 0xa2, 0xea, 0x8a, 0x9, 0x83, 0x1f, 0xfb, 0x89, 0xf, 0xe1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := CreateCommitment(tt.minSquareSize, tt.namespace, tt.message)
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
// the PayForDatas malleated from the original WirePayForData are actually
// valid.
func TestSignMalleatedTxs(t *testing.T) {
	type test struct {
		name       string
		namespace  []byte
		msg        []byte
		squareSize []uint64
		options    []TxBuilderOption
	}

	kb := generateKeyring(t, "test")

	signer := NewKeyringSigner(kb, "test", "test-chain-id")

	tests := []test{
		{
			name:       "single share",
			namespace:  []byte{1, 1, 1, 1, 1, 1, 1, 1},
			msg:        bytes.Repeat([]byte{1}, appconsts.SparseShareContentSize),
			squareSize: []uint64{2, 4, 8, 16},
			options:    []TxBuilderOption{SetGasLimit(2000000)},
		},
		{
			name:       "12 shares",
			namespace:  []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:        bytes.Repeat([]byte{2}, (appconsts.SparseShareContentSize*12)-4), // subtract a few bytes for the delimiter
			squareSize: []uint64{4, 8, 16, 64},
			options: []TxBuilderOption{
				SetGasLimit(123456789),
				SetFeeAmount(sdk.NewCoins(sdk.NewCoin("ucls", sdk.NewInt(987654321)))),
			},
		},
		{
			name:       "10000 bytes",
			namespace:  []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:        bytes.Repeat([]byte{1, 2, 3, 4, 5}, 10000), // subtract a few bytes for the delimiter
			squareSize: AllSquareSizes(),
			options: []TxBuilderOption{
				SetGasLimit(123456789),
				SetFeeAmount(sdk.NewCoins(sdk.NewCoin("ucls", sdk.NewInt(987654321)))),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wpfd, err := NewWirePayForData(tt.namespace, tt.msg, appconsts.MinSquareSize)
			require.NoError(t, err, tt.name)

			err = wpfd.SignMessageShareCommitment(signer, tt.options...)
			assert.NoError(t, err)
			// the signature should exist
			assert.Equal(t, len(wpfd.MessageShareCommitment.Signature), 64)

			signerData, err := signer.GetSignerData()
			require.NoError(t, err)

			wpfdTx, err := signer.BuildSignedTx(signer.NewTxBuilder(tt.options...), wpfd)
			require.NoError(t, err)

			// VerifyPFDSigs goes through the entire malleation process for every
			// square size, creating PFDs from the WirePFDs and check that the
			// signature is valid
			valid, err := VerifyPFDSig(signerData, signer.encCfg.TxConfig, wpfdTx)
			assert.NoError(t, err)
			assert.True(t, valid, tt.name)
		})
	}
}

func TestValidateBasic(t *testing.T) {
	type test struct {
		name    string
		msg     *MsgPayForData
		wantErr *sdkerrors.Error
	}

	validMsg := validMsgPayForData(t)

	// MsgPayForData that uses parity shares namespace id
	paritySharesMsg := validMsgPayForData(t)
	paritySharesMsg.MessageNamespaceId = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	// MsgPayForData that uses tail padding namespace id
	tailPaddingMsg := validMsgPayForData(t)
	tailPaddingMsg.MessageNamespaceId = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE}

	// MsgPayForData that uses transaction namespace id
	txNamespaceMsg := validMsgPayForData(t)
	txNamespaceMsg.MessageNamespaceId = namespace.ID{0, 0, 0, 0, 0, 0, 0, 1}

	// MsgPayForData that uses intermediateStateRoots namespace id
	intermediateStateRootsNamespaceMsg := validMsgPayForData(t)
	intermediateStateRootsNamespaceMsg.MessageNamespaceId = namespace.ID{0, 0, 0, 0, 0, 0, 0, 2}

	// MsgPayForData that uses evidence namespace id
	evidenceNamespaceMsg := validMsgPayForData(t)
	evidenceNamespaceMsg.MessageNamespaceId = namespace.ID{0, 0, 0, 0, 0, 0, 0, 3}

	// MsgPayForData that uses the max reserved namespace id
	maxReservedNamespaceMsg := validMsgPayForData(t)
	maxReservedNamespaceMsg.MessageNamespaceId = namespace.ID{0, 0, 0, 0, 0, 0, 0, 255}

	// MsgPayForData that has no message share commitments
	noMessageShareCommitments := validMsgPayForData(t)
	noMessageShareCommitments.MessageShareCommitment = []byte{}

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

// totalMsgSize subtracts the delimiter size from the desired total size. this
// is useful for testing for messages that occupy exactly so many shares.
func totalMsgSize(size int) int {
	return size - shares.DelimLen(uint64(size))
}

func validWirePayForData(t *testing.T) *MsgWirePayForData {
	message := bytes.Repeat([]byte{1}, 2000)
	msg, err := NewWirePayForData(
		[]byte{1, 2, 3, 4, 5, 6, 7, 8},
		message,
		appconsts.MinSquareSize,
	)
	if err != nil {
		panic(err)
	}

	signer := generateKeyringSigner(t)

	err = msg.SignMessageShareCommitment(signer)
	if err != nil {
		panic(err)
	}
	return msg
}

func validMsgPayForData(t *testing.T) *MsgPayForData {
	kb := generateKeyring(t, "test")
	signer := NewKeyringSigner(kb, "test", "chain-id")
	ns := []byte{1, 1, 1, 1, 1, 1, 1, 2}
	msg := bytes.Repeat([]byte{2}, totalMsgSize(appconsts.SparseShareContentSize*12))
	squareSize := uint64(4)

	wpfd, err := NewWirePayForData(ns, msg, int(squareSize))
	assert.NoError(t, err)

	err = wpfd.SignMessageShareCommitment(signer)
	assert.NoError(t, err)

	_, spfd, _, err := ProcessWirePayForData(wpfd, squareSize)
	require.NoError(t, err)

	return spfd
}
