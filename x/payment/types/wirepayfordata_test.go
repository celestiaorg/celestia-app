package types

import (
	"testing"

	sdkerrors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
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
