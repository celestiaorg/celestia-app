package types_test

import (
	"bytes"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v7/x/zkism/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestMsgCreateInterchainSecurityModuleValidateBasic(t *testing.T) {
	var msg *types.MsgCreateInterchainSecurityModule

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
			name: "invalid trusted state, length too small",
			mallate: func() {
				msg.State = []byte{0x01}
			},
			expErr: types.ErrInvalidTrustedState,
		},
		{
			name: "invalid trusted state, length too large",
			mallate: func() {
				msg.State = bytes.Repeat([]byte{0x01}, types.MaxStateBytes+1)
			},
			expErr: types.ErrInvalidTrustedState,
		},
		{
			name: "invalid merkle tree address length",
			mallate: func() {
				msg.MerkleTreeAddress = []byte{0x01}
			},
			expErr: types.ErrInvalidMerkleTreeAddress,
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
			msg = &types.MsgCreateInterchainSecurityModule{
				State:               bytes.Repeat([]byte{0x01}, 32),
				MerkleTreeAddress:   bytes.Repeat([]byte{0x01}, 32),
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

func TestMsgUpdateInterchainSecurityModuleValidateBasic(t *testing.T) {
	var msg *types.MsgUpdateInterchainSecurityModule

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
		{
			name: "public values too large",
			malleate: func() {
				msg.PublicValues = bytes.Repeat([]byte{0x01}, types.MaxStateTransitionValuesBytes+1)
			},
			expErr: types.ErrInvalidPublicValuesLength,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg = &types.MsgUpdateInterchainSecurityModule{
				Id:           util.CreateMockHexAddress("module", 1),
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
		{
			name: "public values too large",
			malleate: func() {
				msg.PublicValues = bytes.Repeat([]byte{0x01}, types.MaxStateMembershipValuesBytes+1)
			},
			expErr: types.ErrInvalidPublicValuesLength,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg = &types.MsgSubmitMessages{
				Id:           util.CreateMockHexAddress("module", 1),
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
