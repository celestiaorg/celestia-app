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

func TestMountainRange(t *testing.T) {
	type test struct {
		l, squareSize uint64
		expected      []uint64
	}
	tests := []test{
		{
			l:          11,
			squareSize: 4,
			expected:   []uint64{4, 4, 2, 1},
		},
		{
			l:          2,
			squareSize: 64,
			expected:   []uint64{2},
		},
		{ // should this test throw an error? we
			l:          64,
			squareSize: 8,
			expected:   []uint64{8, 8, 8, 8, 8, 8, 8, 8},
		},
	}
	for _, tt := range tests {
		res := powerOf2MountainRange(tt.l, tt.squareSize)
		assert.Equal(t, tt.expected, res)
	}
}

func TestNextLowestPowerOf2(t *testing.T) {
	type test struct {
		input    uint64
		expected uint64
	}
	tests := []test{
		{
			input:    0,
			expected: 0,
		},
		{
			input:    1,
			expected: 1,
		},
		{
			input:    2,
			expected: 2,
		},
		{
			input:    5,
			expected: 4,
		},
		{
			input:    11,
			expected: 8,
		},
		{
			input:    511,
			expected: 256,
		},
	}
	for _, tt := range tests {
		res := nextLowerPowerOf2(tt.input)
		assert.Equal(t, tt.expected, res)
	}
}

func TestNextHighestPowerOf2(t *testing.T) {
	type test struct {
		input    uint64
		expected uint64
	}
	tests := []test{
		{
			input:    0,
			expected: 0,
		},
		{
			input:    1,
			expected: 2,
		},
		{
			input:    2,
			expected: 4,
		},
		{
			input:    5,
			expected: 8,
		},
		{
			input:    11,
			expected: 16,
		},
		{
			input:    511,
			expected: 512,
		},
	}
	for _, tt := range tests {
		res := NextHigherPowerOf2(tt.input)
		assert.Equal(t, tt.expected, res)
	}
}

func TestPowerOf2(t *testing.T) {
	type test struct {
		input    uint64
		expected bool
	}
	tests := []test{
		{
			input:    1,
			expected: true,
		},
		{
			input:    2,
			expected: true,
		},
		{
			input:    256,
			expected: true,
		},
		{
			input:    3,
			expected: false,
		},
		{
			input:    79,
			expected: false,
		},
		{
			input:    0,
			expected: false,
		},
	}
	for _, tt := range tests {
		res := powerOf2(tt.input)
		assert.Equal(t, tt.expected, res)
	}
}

// TestCreateCommitment only shows if something changed, it doesn't actually
// show that the commit is being created correctly todo(evan): fix me.
func TestCreateCommitment(t *testing.T) {
	type test struct {
		squareSize uint64
		namespace  []byte
		message    []byte
		expected   []byte
		expectErr  bool
	}
	tests := []test{
		{
			squareSize: 4,
			namespace:  bytes.Repeat([]byte{0xFF}, 8),
			message:    bytes.Repeat([]byte{0xFF}, 11*ShareSize),
			expected:   []byte{0x6a, 0x29, 0xa3, 0xfa, 0x6a, 0xe3, 0x68, 0x9b, 0xce, 0xc8, 0x30, 0x1d, 0x32, 0xe5, 0x25, 0x1c, 0xad, 0x38, 0xa1, 0xde, 0x6b, 0xf5, 0xb9, 0xca, 0xec, 0x4c, 0x17, 0xe3, 0xbf, 0x77, 0x3, 0x2f},
		},
		{
			squareSize: 2,
			namespace:  bytes.Repeat([]byte{0xFF}, 8),
			message:    bytes.Repeat([]byte{0xFF}, 100*ShareSize),
			expectErr:  true,
		},
	}
	for _, tt := range tests {
		res, err := CreateCommitment(tt.squareSize, tt.namespace, tt.message)
		if tt.expectErr {
			assert.Error(t, err)
			continue
		}
		assert.NoError(t, err)
		assert.Equal(t, tt.expected, res)
	}
}

// TestSignMalleatedTxs checks to see that the signatures that are generated for
// the PayForDatas malleated from the original WirePayForData are actually
// valid.
func TestSignMalleatedTxs(t *testing.T) {
	type test struct {
		name    string
		ns, msg []byte
		ss      []uint64
		options []TxBuilderOption
	}

	kb := generateKeyring(t, "test")

	signer := NewKeyringSigner(kb, "test", "test-chain-id")

	tests := []test{
		{
			name:    "single share",
			ns:      []byte{1, 1, 1, 1, 1, 1, 1, 1},
			msg:     bytes.Repeat([]byte{1}, appconsts.SparseShareContentSize),
			ss:      []uint64{2, 4, 8, 16},
			options: []TxBuilderOption{SetGasLimit(2000000)},
		},
		{
			name: "12 shares",
			ns:   []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:  bytes.Repeat([]byte{2}, (appconsts.SparseShareContentSize*12)-4), // subtract a few bytes for the delimiter
			ss:   []uint64{4, 8, 16, 64},
			options: []TxBuilderOption{
				SetGasLimit(123456789),
				SetFeeAmount(sdk.NewCoins(sdk.NewCoin("utia", sdk.NewInt(987654321)))),
			},
		},
		{
			name: "12 shares",
			ns:   []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:  bytes.Repeat([]byte{1, 2, 3, 4, 5}, 10000), // subtract a few bytes for the delimiter
			ss:   AllSquareSizes(50000),
			options: []TxBuilderOption{
				SetGasLimit(123456789),
				SetFeeAmount(sdk.NewCoins(sdk.NewCoin("utia", sdk.NewInt(987654321)))),
			},
		},
	}

	for _, tt := range tests {
		wpfd, err := NewWirePayForData(tt.ns, tt.msg, tt.ss...)
		require.NoError(t, err, tt.name)
		err = wpfd.SignShareCommitments(signer, tt.options...)
		// there should be no error
		assert.NoError(t, err)
		// the signature should exist
		assert.Equal(t, len(wpfd.MessageShareCommitment[0].Signature), 64)

		sData, err := signer.GetSignerData()
		require.NoError(t, err)

		wpfdTx, err := signer.BuildSignedTx(signer.NewTxBuilder(tt.options...), wpfd)
		require.NoError(t, err)

		// VerifyPFDSigs goes through the entire malleation process for every
		// square size, creating PfDs from the wirePfD and check that the
		// signature is valid
		valid, err := VerifyPFDSigs(sData, signer.encCfg.TxConfig, wpfdTx)
		assert.NoError(t, err)
		assert.True(t, valid, tt.name)
	}
}

func TestProcessMessage(t *testing.T) {
	type test struct {
		name      string
		ns, msg   []byte
		ss        uint64
		expectErr bool
		modify    func(*MsgWirePayForData) *MsgWirePayForData
	}

	dontModify := func(in *MsgWirePayForData) *MsgWirePayForData {
		return in
	}

	kb := generateKeyring(t, "test")

	signer := NewKeyringSigner(kb, "test", "chain-id")

	tests := []test{
		{
			name:   "single share square size 2",
			ns:     []byte{1, 1, 1, 1, 1, 1, 1, 1},
			msg:    bytes.Repeat([]byte{1}, totalMsgSize(appconsts.SparseShareContentSize)),
			ss:     2,
			modify: dontModify,
		},
		{
			name:   "15 shares square size 4",
			ns:     []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:    bytes.Repeat([]byte{2}, totalMsgSize(appconsts.SparseShareContentSize*15)),
			ss:     4,
			modify: dontModify,
		},
		{
			name: "incorrect square size",
			ns:   []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:  bytes.Repeat([]byte{2}, totalMsgSize(appconsts.SparseShareContentSize*15)),
			ss:   4,
			modify: func(wpfd *MsgWirePayForData) *MsgWirePayForData {
				wpfd.MessageShareCommitment[0].SquareSize = 99999
				return wpfd
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		wpfd, err := NewWirePayForData(tt.ns, tt.msg, tt.ss)
		require.NoError(t, err, tt.name)
		err = wpfd.SignShareCommitments(signer)
		assert.NoError(t, err)

		wpfd = tt.modify(wpfd)

		message, spfd, sig, err := ProcessWirePayForData(wpfd, tt.ss)
		if tt.expectErr {
			assert.Error(t, err, tt.name)
			continue
		}

		// ensure that the shared fields are identical
		assert.Equal(t, tt.msg, message.Data, tt.name)
		assert.Equal(t, tt.ns, message.NamespaceId, tt.name)
		assert.Equal(t, wpfd.Signer, spfd.Signer, tt.name)
		assert.Equal(t, wpfd.MessageNamespaceId, spfd.MessageNamespaceId, tt.name)
		assert.Equal(t, wpfd.MessageShareCommitment[0].ShareCommitment, spfd.MessageShareCommitment, tt.name)
		assert.Equal(t, wpfd.MessageShareCommitment[0].Signature, sig, tt.name)
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
		{
			name:    "no message share commitments",
			msg:     noMessageShareCommitments,
			wantErr: ErrNoMessageShareCommitments,
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
		AllSquareSizes(len(message))...,
	)
	if err != nil {
		panic(err)
	}

	signer := generateKeyringSigner(t)

	err = msg.SignShareCommitments(signer)
	if err != nil {
		panic(err)
	}
	return msg
}

func validMsgPayForData(t *testing.T) *MsgPayForData {
	kb := generateKeyring(t, "test")
	signer := NewKeyringSigner(kb, "test", "chain-id")
	ns := []byte{1, 1, 1, 1, 1, 1, 1, 2}
	msg := bytes.Repeat([]byte{2}, totalMsgSize(appconsts.SparseShareContentSize*15))
	ss := uint64(4)

	wpfd, err := NewWirePayForData(ns, msg, ss)
	assert.NoError(t, err)

	err = wpfd.SignShareCommitments(signer)
	assert.NoError(t, err)

	_, spfd, _, err := ProcessWirePayForData(wpfd, ss)
	require.NoError(t, err)

	return spfd
}
