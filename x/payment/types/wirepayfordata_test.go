package types

import (
	"bytes"
	"testing"

	sdkerrors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWirePayForData_ValidateBasic(t *testing.T) {
	type test struct {
		name    string
		msg     *MsgWirePayForData
		wantErr *sdkerrors.Error
	}

	// valid pfd
	validMsg := validWirePayForData(t)

	// pfd with bad ns id
	badIDMsg := validWirePayForData(t)
	badIDMsg.MessageNamespaceId = []byte{1, 2, 3, 4, 5, 6, 7}

	// pfd that uses reserved ns id
	reservedMsg := validWirePayForData(t)
	reservedMsg.MessageNamespaceId = []byte{0, 0, 0, 0, 0, 0, 0, 100}

	// pfd that uses parity shares namespace id
	paritySharesMsg := validWirePayForData(t)
	paritySharesMsg.MessageNamespaceId = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	// pfd that uses parity shares namespace id
	tailPaddingMsg := validWirePayForData(t)
	tailPaddingMsg.MessageNamespaceId = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE}

	// pfd that has a wrong msg size
	invalidDeclaredMsgSizeMsg := validWirePayForData(t)
	invalidDeclaredMsgSizeMsg.MessageSize = 999

	// pfd with bad commitment
	badCommitMsg := validWirePayForData(t)
	badCommitMsg.MessageShareCommitment.ShareCommitment = []byte{1, 2, 3, 4}

	tests := []test{
		{
			name:    "valid msg",
			msg:     validMsg,
			wantErr: nil,
		},
		{
			name:    "bad ns ID",
			msg:     badIDMsg,
			wantErr: ErrInvalidNamespaceLen,
		},
		{
			name:    "reserved ns id",
			msg:     reservedMsg,
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "bad declared message size",
			msg:     invalidDeclaredMsgSizeMsg,
			wantErr: ErrDeclaredActualDataSizeMismatch,
		},
		{
			name:    "bad commitment",
			msg:     badCommitMsg,
			wantErr: ErrInvalidShareCommit,
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

func TestMsgMinSquareSize(t *testing.T) {
	type testCase struct {
		name     string
		msgLen   uint64
		expected uint64
	}
	tests := []testCase{
		{
			name:     "1 byte",
			msgLen:   1,
			expected: 1,
		},
		{
			name:     "100 bytes",
			msgLen:   100,
			expected: 1,
		},
		{
			name:     "2 sparse shares",
			msgLen:   appconsts.SparseShareContentSize * 2,
			expected: 2,
		},
		{
			name:     "4 sparse shares",
			msgLen:   appconsts.SparseShareContentSize * 4,
			expected: 4,
		},
		{
			name:     "16 sparse shares",
			msgLen:   appconsts.SparseShareContentSize * 16,
			expected: 8,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MsgMinSquareSize(tc.msgLen)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestProcessWirePayForData(t *testing.T) {
	type test struct {
		name       string
		namespace  []byte
		msg        []byte
		squareSize uint64
		expectErr  bool
		modify     func(*MsgWirePayForData) *MsgWirePayForData
	}

	dontModify := func(in *MsgWirePayForData) *MsgWirePayForData {
		return in
	}

	kb := generateKeyring(t, "test")

	signer := NewKeyringSigner(kb, "test", "chain-id")

	tests := []test{
		{
			name:       "single share square size 2",
			namespace:  []byte{1, 1, 1, 1, 1, 1, 1, 1},
			msg:        bytes.Repeat([]byte{1}, totalMsgSize(appconsts.SparseShareContentSize)),
			squareSize: 2,
			modify:     dontModify,
		},
		{
			name:       "12 shares square size 4",
			namespace:  []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:        bytes.Repeat([]byte{2}, totalMsgSize(appconsts.SparseShareContentSize*12)),
			squareSize: 4,
			modify:     dontModify,
		},
	}

	for _, tt := range tests {
		wpfd, err := NewWirePayForData(tt.namespace, tt.msg)
		require.NoError(t, err, tt.name)
		err = wpfd.SignShareCommitments(signer)
		assert.NoError(t, err)

		wpfd = tt.modify(wpfd)

		message, spfd, sig, err := ProcessWirePayForData(wpfd)
		if tt.expectErr {
			assert.Error(t, err, tt.name)
			continue
		}

		// ensure that the shared fields are identical
		assert.Equal(t, tt.msg, message.Data, tt.name)
		assert.Equal(t, tt.namespace, message.NamespaceId, tt.name)
		assert.Equal(t, wpfd.Signer, spfd.Signer, tt.name)
		assert.Equal(t, wpfd.MessageNamespaceId, spfd.MessageNamespaceId, tt.name)
		assert.Equal(t, wpfd.MessageShareCommitment.ShareCommitment, spfd.MessageShareCommitment, tt.name)
		assert.Equal(t, wpfd.MessageShareCommitment.Signature, sig, tt.name)
	}
}
