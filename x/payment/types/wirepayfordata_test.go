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
	badCommitMsg.MessageShareCommitment[0].ShareCommitment = []byte{1, 2, 3, 4}

	// pfd that has invalid square size (not power of 2)
	invalidSquareSizeMsg := validWirePayForData(t)
	invalidSquareSizeMsg.MessageShareCommitment[0].SquareSize = 15

	// pfd that signs over all squares but the first one
	missingCommitmentForOneSquareSize := validWirePayForData(t)
	missingCommitmentForOneSquareSize.MessageShareCommitment = missingCommitmentForOneSquareSize.MessageShareCommitment[1:]

	// pfd that signed over no squares
	noMessageShareCommitments := validWirePayForData(t)
	noMessageShareCommitments.MessageShareCommitment = []ShareCommitAndSignature{}

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
			name:    "invalid square size",
			msg:     invalidSquareSizeMsg,
			wantErr: ErrCommittedSquareSizeNotPowOf2,
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
			name:    "no message share commitments",
			msg:     noMessageShareCommitments,
			wantErr: ErrNoMessageShareCommitments,
		},
		{
			name:    "missing commitment for one square size",
			msg:     missingCommitmentForOneSquareSize,
			wantErr: ErrInvalidShareCommitments,
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
		{
			name:       "incorrect square size",
			namespace:  []byte{1, 1, 1, 1, 1, 1, 1, 2},
			msg:        bytes.Repeat([]byte{2}, totalMsgSize(appconsts.SparseShareContentSize*12)),
			squareSize: 4,
			modify: func(wpfd *MsgWirePayForData) *MsgWirePayForData {
				wpfd.MessageShareCommitment[0].SquareSize = 99999
				return wpfd
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		wpfd, err := NewWirePayForData(tt.namespace, tt.msg, tt.squareSize)
		require.NoError(t, err, tt.name)
		err = wpfd.SignShareCommitments(signer)
		assert.NoError(t, err)

		wpfd = tt.modify(wpfd)

		message, spfd, sig, err := ProcessWirePayForData(wpfd, tt.squareSize)
		if tt.expectErr {
			assert.Error(t, err, tt.name)
			continue
		}

		// ensure that the shared fields are identical
		assert.Equal(t, tt.msg, message.Data, tt.name)
		assert.Equal(t, tt.namespace, message.NamespaceId, tt.name)
		assert.Equal(t, wpfd.Signer, spfd.Signer, tt.name)
		assert.Equal(t, wpfd.MessageNamespaceId, spfd.MessageNamespaceId, tt.name)
		assert.Equal(t, wpfd.MessageShareCommitment[0].ShareCommitment, spfd.MessageShareCommitment, tt.name)
		assert.Equal(t, wpfd.MessageShareCommitment[0].Signature, sig, tt.name)
	}
}
