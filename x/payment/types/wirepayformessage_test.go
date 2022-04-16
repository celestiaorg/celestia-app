package types

import (
	"bytes"
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/assert"
	"github.com/tendermint/tendermint/pkg/consts"
)

func TestWirePayForMessage_ValidateBasic(t *testing.T) {
	type test struct {
		name    string
		msg     *MsgWirePayForMessage
		wantErr *sdkerrors.Error
	}

	// valid pfm
	validMsg := validWirePayForMessage(t)

	// pfm with bad ns id
	badIDMsg := validWirePayForMessage(t)
	badIDMsg.MessageNameSpaceId = []byte{1, 2, 3, 4, 5, 6, 7}

	// pfm that uses reserved ns id
	reservedMsg := validWirePayForMessage(t)
	reservedMsg.MessageNameSpaceId = []byte{0, 0, 0, 0, 0, 0, 0, 100}

	// pfm that has a wrong msg size
	invalidMsgSizeMsg := validWirePayForMessage(t)
	invalidMsgSizeMsg.Message = bytes.Repeat([]byte{1}, consts.ShareSize-20)

	// pfm that has a wrong msg size
	invalidDeclaredMsgSizeMsg := validWirePayForMessage(t)
	invalidDeclaredMsgSizeMsg.MessageSize = 999

	// pfm with bad commitment
	badCommitMsg := validWirePayForMessage(t)
	badCommitMsg.MessageShareCommitment[0].ShareCommitment = []byte{1, 2, 3, 4}

	// pfm that has invalid square size (not power of 2)
	invalidSquareSizeMsg := validWirePayForMessage(t)
	invalidSquareSizeMsg.MessageShareCommitment[0].K = 15

	// pfm that has a different power of 2 square size
	badSquareSizeMsg := validWirePayForMessage(t)
	badSquareSizeMsg.MessageShareCommitment[0].K = 4

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
			name:    "invalid msg size",
			msg:     invalidMsgSizeMsg,
			wantErr: ErrInvalidDataSize,
		},
		{
			name:    "bad declared message size",
			msg:     invalidDeclaredMsgSizeMsg,
			wantErr: ErrDeclaredActualDataSizeMismatch,
		},
		{
			name:    "bad commitment",
			msg:     badCommitMsg,
			wantErr: ErrCommittedSquareSizeNotPowOf2,
		},
		{
			name:    "invalid square size",
			msg:     invalidSquareSizeMsg,
			wantErr: ErrCommittedSquareSizeNotPowOf2,
		},
		{
			name:    "wrong but valid square size",
			msg:     badSquareSizeMsg,
			wantErr: ErrInvalidShareCommit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr != nil {
				assert.ErrorAs(t, err, tt.wantErr)
				space, code, log := sdkerrors.ABCIInfo(err, false)
				assert.Equal(t, tt.wantErr.Codespace(), space)
				assert.Equal(t, tt.wantErr.ABCICode(), code)
				t.Log(log)
			}
		})
	}
}
