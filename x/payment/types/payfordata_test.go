package types

import (
	"bytes"
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/pkg/consts"
)

func TestMountainRange(t *testing.T) {
	type test struct {
		l, k     uint64
		expected []uint64
	}
	tests := []test{
		{
			l:        11,
			k:        4,
			expected: []uint64{4, 4, 2, 1},
		},
		{
			l:        2,
			k:        64,
			expected: []uint64{2},
		},
		{ // should this test throw an error? we
			l:        64,
			k:        8,
			expected: []uint64{8, 8, 8, 8, 8, 8, 8, 8},
		},
	}
	for _, tt := range tests {
		res := powerOf2MountainRange(tt.l, tt.k)
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
			input:    2,
			expected: 2,
		},
		{
			input:    11,
			expected: 8,
		},
		{
			input:    511,
			expected: 256,
		},
		{
			input:    1,
			expected: 1,
		},
		{
			input:    0,
			expected: 0,
		},
	}
	for _, tt := range tests {
		res := nextLowestPowerOf2(tt.input)
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
			input:    2,
			expected: 4,
		},
		{
			input:    11,
			expected: 16,
		},
		{
			input:    511,
			expected: 512,
		},
		{
			input:    1,
			expected: 2,
		},
		{
			input:    0,
			expected: 0,
		},
	}
	for _, tt := range tests {
		res := NextHighestPowerOf2(tt.input)
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

// TestCreateCommit only shows if something changed, it doesn't actually show
// the commit is being created correctly todo(evan): fix me.
func TestCreateCommitment(t *testing.T) {
	type test struct {
		k         uint64
		namespace []byte
		message   []byte
		expected  []byte
		expectErr bool
	}
	tests := []test{
		{
			k:         4,
			namespace: bytes.Repeat([]byte{0xFF}, 8),
			message:   bytes.Repeat([]byte{0xFF}, 11*ShareSize),
			expected:  []byte{0xf2, 0xd4, 0xfc, 0x39, 0x4e, 0xf3, 0x97, 0x9d, 0xf4, 0x4c, 0x99, 0x87, 0x36, 0x7d, 0x7d, 0x4, 0xf2, 0xa7, 0x89, 0x26, 0x6d, 0xf5, 0x78, 0xe1, 0xff, 0x72, 0xb4, 0x75, 0x12, 0x1e, 0x71, 0xc3},
		},
		{
			k:         2,
			namespace: bytes.Repeat([]byte{0xFF}, 8),
			message:   bytes.Repeat([]byte{0xFF}, 100*ShareSize),
			expectErr: true,
		},
	}
	for _, tt := range tests {
		res, err := CreateCommitment(tt.k, tt.namespace, tt.message)
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
			msg:     bytes.Repeat([]byte{1}, consts.MsgShareSize),
			ss:      []uint64{2, 4, 8, 16},
			options: []TxBuilderOption{SetGasLimit(2000000)},
		},
		{
			name: "12 shares",
			ns:   []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:  bytes.Repeat([]byte{2}, (consts.MsgShareSize*12)-4), // subtract a few bytes for the delimiter
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
			msg:    bytes.Repeat([]byte{1}, totalMsgSize(consts.MsgShareSize)),
			ss:     2,
			modify: dontModify,
		},
		{
			name:   "15 shares square size 4",
			ns:     []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:    bytes.Repeat([]byte{2}, totalMsgSize(consts.MsgShareSize*15)),
			ss:     4,
			modify: dontModify,
		},
		{
			name: "incorrect square size",
			ns:   []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:  bytes.Repeat([]byte{2}, totalMsgSize(consts.MsgShareSize*15)),
			ss:   4,
			modify: func(wpfd *MsgWirePayForData) *MsgWirePayForData {
				wpfd.MessageShareCommitment[0].K = 99999
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
		assert.Equal(t, wpfd.MessageNameSpaceId, spfd.MessageNamespaceId, tt.name)
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
	return size - DelimLen(uint64(size))
}

func validWirePayForData(t *testing.T) *MsgWirePayForData {
	msg, err := NewWirePayForData(
		[]byte{1, 2, 3, 4, 5, 6, 7, 8},
		bytes.Repeat([]byte{1}, 2000),
		16, 32, 64,
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
	msg := bytes.Repeat([]byte{2}, totalMsgSize(consts.MsgShareSize*15))
	ss := uint64(4)

	wpfd, err := NewWirePayForData(ns, msg, ss)
	assert.NoError(t, err)

	err = wpfd.SignShareCommitments(signer)
	assert.NoError(t, err)

	_, spfd, _, err := ProcessWirePayForData(wpfd, ss)
	require.NoError(t, err)

	return spfd
}
