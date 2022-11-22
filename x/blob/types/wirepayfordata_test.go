package types

import (
	"bytes"
	"testing"

	sdkerrors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWirePayForBlob_ValidateBasic(t *testing.T) {
	type test struct {
		name    string
		msg     *MsgWirePayForBlob
		wantErr *sdkerrors.Error
	}

	// valid pfb
	validMsg := validWirePayForBlob(t)

	// pfb with bad ns id
	badIDMsg := validWirePayForBlob(t)
	badIDMsg.NamespaceId = []byte{1, 2, 3, 4, 5, 6, 7}

	// pfb that uses reserved ns id
	reservedMsg := validWirePayForBlob(t)
	reservedMsg.NamespaceId = []byte{0, 0, 0, 0, 0, 0, 0, 100}

	// pfb that uses parity shares namespace id
	paritySharesMsg := validWirePayForBlob(t)
	paritySharesMsg.NamespaceId = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	// pfb that uses parity shares namespace id
	tailPaddingMsg := validWirePayForBlob(t)
	tailPaddingMsg.NamespaceId = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE}

	// pfb that has a wrong msg size
	invalidDeclaredMsgSizeMsg := validWirePayForBlob(t)
	invalidDeclaredMsgSizeMsg.BlobSize = 999

	// pfb with bad commitment
	badCommitMsg := validWirePayForBlob(t)
	badCommitMsg.ShareCommitment.ShareCommitment = []byte{1, 2, 3, 4}

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

func TestProcessWirePayForBlob(t *testing.T) {
	type test struct {
		name      string
		namespace []byte
		msg       []byte
		expectErr bool
		modify    func(*MsgWirePayForBlob) *MsgWirePayForBlob
	}

	dontModify := func(in *MsgWirePayForBlob) *MsgWirePayForBlob {
		return in
	}

	kb := generateKeyring(t, "test")

	signer := NewKeyringSigner(kb, "test", "chain-id")

	tests := []test{
		{
			name:      "single share square size 2",
			namespace: []byte{1, 1, 1, 1, 1, 1, 1, 1},
			msg:       bytes.Repeat([]byte{1}, totalMsgSize(appconsts.SparseShareContentSize)),
			modify:    dontModify,
		},
		{
			name:      "12 shares square size 4",
			namespace: []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:       bytes.Repeat([]byte{2}, totalMsgSize(appconsts.SparseShareContentSize*12)),
			modify:    dontModify,
		},
		{
			name:      "empty message",
			namespace: []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:       []byte{},
			modify:    dontModify,
		},
	}

	for _, tt := range tests {
		wpfb, err := NewWirePayForBlob(tt.namespace, tt.msg, appconsts.ShareVersionZero)
		require.NoError(t, err, tt.name)
		err = wpfb.SignShareCommitment(signer)
		assert.NoError(t, err)

		wpfb = tt.modify(wpfb)

		message, spfb, sig, err := ProcessWirePayForBlob(wpfb)
		if tt.expectErr {
			assert.Error(t, err, tt.name)
			continue
		}

		// ensure that the shared fields are identical
		assert.Equal(t, tt.msg, message.Data, tt.name)
		assert.Equal(t, tt.namespace, message.NamespaceId, tt.name)
		assert.Equal(t, wpfb.Signer, spfb.Signer, tt.name)
		assert.Equal(t, wpfb.NamespaceId, spfb.NamespaceId, tt.name)
		assert.Equal(t, wpfb.ShareCommitment.ShareCommitment, spfb.ShareCommitment, tt.name)
		assert.Equal(t, wpfb.ShareCommitment.Signature, sig, tt.name)
	}
}
