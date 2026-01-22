package types_test

import (
	"bytes"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v7/x/zkism/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestGenesisStateValidate(t *testing.T) {
	groth16Vk := readGroth16Vkey(t)

	tests := []struct {
		name     string
		malleate func(gs *types.GenesisState)
		expErr   error
	}{
		{
			name:     "success",
			malleate: func(gs *types.GenesisState) {},
			expErr:   nil,
		},
		{
			name: "zero ism identifier",
			malleate: func(gs *types.GenesisState) {
				gs.Isms[0].Id = util.NewZeroAddress()
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "duplicate ism id",
			malleate: func(gs *types.GenesisState) {
				gs.Isms = append(gs.Isms, gs.Isms[0])
			},
			expErr: sdkerrors.ErrAppConfig,
		},
		{
			name: "invalid trusted state, length too small",
			malleate: func(gs *types.GenesisState) {
				gs.Isms[0].State = bytes.Repeat([]byte{0x01}, types.MinStateBytes-1)
			},
			expErr: types.ErrInvalidTrustedState,
		},
		{
			name: "invalid trusted state, length too large",
			malleate: func(gs *types.GenesisState) {
				gs.Isms[0].State = bytes.Repeat([]byte{0x01}, types.MaxStateBytes+1)
			},
			expErr: types.ErrInvalidTrustedState,
		},
		{
			name: "invalid merkle tree address length",
			malleate: func(gs *types.GenesisState) {
				gs.Isms[0].MerkleTreeAddress = []byte{0x01}
			},
			expErr: types.ErrInvalidMerkleTreeAddress,
		},
		{
			name: "invalid groth16 verifying key",
			malleate: func(gs *types.GenesisState) {
				gs.Isms[0].Groth16Vkey = []byte{0x01}
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "invalid state transition verifying key length",
			malleate: func(gs *types.GenesisState) {
				gs.Isms[0].StateTransitionVkey = []byte{0x01}
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "invalid state membership verifying key length",
			malleate: func(gs *types.GenesisState) {
				gs.Isms[0].StateMembershipVkey = []byte{0x01}
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "messages zero identifier",
			malleate: func(gs *types.GenesisState) {
				gs.Messages[0].Id = util.NewZeroAddress()
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "messages for unknown ism",
			malleate: func(gs *types.GenesisState) {
				gs.Messages[0].Id = util.CreateMockHexAddress("module", 2)
			},
			expErr: types.ErrIsmNotFound,
		},
		{
			name: "duplicate messages entry",
			malleate: func(gs *types.GenesisState) {
				gs.Messages = append(gs.Messages, gs.Messages[0])
			},
			expErr: sdkerrors.ErrAppConfig,
		},
		{
			name: "invalid message id",
			malleate: func(gs *types.GenesisState) {
				gs.Messages[0].Messages = []string{"0xzz"}
			},
			expErr: sdkerrors.ErrAppConfig,
		},
		{
			name: "invalid message id length",
			malleate: func(gs *types.GenesisState) {
				gs.Messages[0].Messages = []string{types.EncodeHex(bytes.Repeat([]byte{0x01}, 31))}
			},
			expErr: sdkerrors.ErrAppConfig,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := newValidGenesisState(groth16Vk)
			tc.malleate(&gs)

			err := gs.Validate()
			if tc.expErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.expErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func newValidGenesisState(groth16Vk []byte) types.GenesisState {
	ismID := util.CreateMockHexAddress("module", 1)
	return types.GenesisState{
		Isms: []types.InterchainSecurityModule{
			{
				Id:                  ismID,
				Owner:               "owner",
				State:               bytes.Repeat([]byte{0x01}, types.MinStateBytes),
				MerkleTreeAddress:   bytes.Repeat([]byte{0x02}, 32),
				Groth16Vkey:         append([]byte(nil), groth16Vk...),
				StateTransitionVkey: bytes.Repeat([]byte{0x03}, 32),
				StateMembershipVkey: bytes.Repeat([]byte{0x04}, 32),
			},
		},
		Messages: []types.GenesisMessages{
			{
				Id:       ismID,
				Messages: []string{types.EncodeHex(bytes.Repeat([]byte{0x05}, 32))},
			},
		},
	}
}
