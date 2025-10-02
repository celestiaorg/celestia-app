package types_test

import (
	"bytes"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/celestiaorg/go-square/v2/share"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestMsgCreateZKExecutionISMValidateBasic(t *testing.T) {
	var msg *types.MsgCreateZKExecutionISM

	groth16Vk := readGroth16Vkey(t)

	tests := []struct {
		name    string
		mallate func()
		expErr  error
	}{
		{
			name:    "success",
			mallate: func() {},
			expErr:  nil,
		},
		{
			name: "invalid namespace",
			mallate: func() {
				msg.Namespace = []byte{0x01}
			},
			expErr: types.ErrInvalidNamespace,
		},
		{
			name: "invalid sequencer public key length",
			mallate: func() {
				msg.SequencerPublicKey = []byte{0x01}
			},
			expErr: types.ErrInvalidSequencerKey,
		},
		{
			name: "invalid state root length",
			mallate: func() {
				msg.StateRoot = []byte{0x01}
			},
			expErr: types.ErrInvalidStateRoot,
		},
		{
			name: "invalid groth16 verifying key",
			mallate: func() {
				msg.Groth16Vkey = []byte{0x01}
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "invalid state transition verifying key length",
			mallate: func() {
				msg.StateTransitionVkey = []byte{0x01}
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "invalid state membership verifying key length",
			mallate: func() {
				msg.StateMembershipVkey = []byte{0x01}
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg = &types.MsgCreateZKExecutionISM{
				StateRoot:           bytes.Repeat([]byte{0x01}, 32),
				Namespace:           share.MustNewV0Namespace([]byte("namespace")).Bytes(),
				SequencerPublicKey:  bytes.Repeat([]byte{0x01}, 32),
				Groth16Vkey:         groth16Vk,
				StateTransitionVkey: bytes.Repeat([]byte{0x01}, 32),
				StateMembershipVkey: bytes.Repeat([]byte{0x01}, 32),
			}

			tc.mallate()

			err := msg.ValidateBasic()
			if tc.expErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.expErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgUpdateZKExecutionISMValidateBasic(t *testing.T) {
	var msg *types.MsgUpdateZKExecutionISM

	tests := []struct {
		name     string
		malleate func()
		expErr   error
	}{
		{
			name:     "success",
			malleate: func() {},
			expErr:   nil,
		},
		{
			name: "zero ism identifier",
			malleate: func() {
				msg.Id = util.NewZeroAddress()
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "zero height",
			malleate: func() {
				msg.Height = 0
			},
			expErr: types.ErrInvalidHeight,
		},
		{
			name: "invalid proof length",
			malleate: func() {
				msg.Proof = []byte{0x01}
			},
			expErr: types.ErrInvalidProofLength,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg = &types.MsgUpdateZKExecutionISM{
				Id:           util.CreateMockHexAddress("module", 1),
				Height:       1,
				Proof:        bytes.Repeat([]byte{0x01}, types.PrefixLen+types.ProofSize),
				PublicValues: []byte{0x01, 0x02},
			}

			tc.malleate()

			err := msg.ValidateBasic()
			if tc.expErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.expErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgSubmitMessagesValidateBasic(t *testing.T) {
	var msg *types.MsgSubmitMessages

	tests := []struct {
		name     string
		malleate func()
		expErr   error
	}{
		{
			name:     "success",
			malleate: func() {},
			expErr:   nil,
		},
		{
			name: "zero ism identifier",
			malleate: func() {
				msg.Id = util.NewZeroAddress()
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid proof length",
			malleate: func() {
				msg.Proof = []byte{0x01}
			},
			expErr: types.ErrInvalidProofLength,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg = &types.MsgSubmitMessages{
				Id:           util.CreateMockHexAddress("module", 1),
				Height:       1,
				Proof:        bytes.Repeat([]byte{0x01}, types.PrefixLen+types.ProofSize),
				PublicValues: []byte{0x01, 0x02},
			}

			tc.malleate()

			err := msg.ValidateBasic()
			if tc.expErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.expErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
