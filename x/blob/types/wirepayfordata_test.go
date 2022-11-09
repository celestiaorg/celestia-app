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
	badCommitMsg.ShareCommitment[0].ShareCommitment = []byte{1, 2, 3, 4}

	// pfb that has invalid square size (not power of 2)
	invalidSquareSizeMsg := validWirePayForBlob(t)
	invalidSquareSizeMsg.ShareCommitment[0].SquareSize = 15

	// pfb that signs over all squares but the first one
	missingCommitmentForOneSquareSize := validWirePayForBlob(t)
	missingCommitmentForOneSquareSize.ShareCommitment = missingCommitmentForOneSquareSize.ShareCommitment[1:]

	// pfb that signed over no squares
	noMessageShareCommitments := validWirePayForBlob(t)
	noMessageShareCommitments.ShareCommitment = []ShareCommitAndSignature{}

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

func TestProcessWirePayForBlob(t *testing.T) {
	type test struct {
		name       string
		namespace  []byte
		msg        []byte
		squareSize uint64
		expectErr  bool
		modify     func(*MsgWirePayForBlob) *MsgWirePayForBlob
	}

	dontModify := func(in *MsgWirePayForBlob) *MsgWirePayForBlob {
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
			modify: func(wpfb *MsgWirePayForBlob) *MsgWirePayForBlob {
				wpfb.ShareCommitment[0].SquareSize = 99999
				return wpfb
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		wpfb, err := NewWirePayForBlob(tt.namespace, tt.msg, tt.squareSize)
		require.NoError(t, err, tt.name)
		err = wpfb.SignShareCommitments(signer)
		assert.NoError(t, err)

		wpfb = tt.modify(wpfb)

		message, spfb, sig, err := ProcessWirePayForBlob(wpfb, tt.squareSize)
		if tt.expectErr {
			assert.Error(t, err, tt.name)
			continue
		}

		// ensure that the shared fields are identical
		assert.Equal(t, tt.msg, message.Data, tt.name)
		assert.Equal(t, tt.namespace, message.NamespaceId, tt.name)
		assert.Equal(t, wpfb.Signer, spfb.Signer, tt.name)
		assert.Equal(t, wpfb.NamespaceId, spfb.NamespaceId, tt.name)
		assert.Equal(t, wpfb.ShareCommitment[0].ShareCommitment, spfb.ShareCommitment, tt.name)
		assert.Equal(t, wpfb.ShareCommitment[0].Signature, sig, tt.name)
	}
}
