package types_test

import (
	"bytes"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
			name: "success: maximum root history size",
			mallate: func() {
				msg.RootHistorySize = types.MaxRootHistorySize
			},
			expErr: nil,
		},
		{
			name: "root history size above the maximum",
			mallate: func() {
				msg.RootHistorySize = types.MaxRootHistorySize + 1
			},
			expErr: types.ErrInvalidRootHistorySize,
		},
		{
			name: "invalid groth16 verifying key",
			mallate: func() {
				msg.Groth16Vkey = []byte{0x01}
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "groth16 vkey with inflated G1.K length is rejected before deserialization",
			mallate: func() {
				// Craft a 292-byte payload: valid curve points (288 bytes from
				// the real VK) + uint32 0xFFFFFFFF as the G1.K length prefix.
				// Before the fix, this would allocate ~256 GiB in
				// NewVerifyingKey. The size check now rejects it immediately.
				malicious := make([]byte, 292)
				copy(malicious, groth16Vk[:288])
				malicious[288] = 0xFF
				malicious[289] = 0xFF
				malicious[290] = 0xFF
				malicious[291] = 0xFF
				msg.Groth16Vkey = malicious
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "492-byte vkey with inflated G1.K length bypasses size check",
			mallate: func() {
				// Craft a payload that is exactly Groth16VkeySize (492 bytes)
				// so it passes the length check, but set the G1.K length prefix
				// at bytes 288-291 to 0xFFFFFFFF. This tests whether the
				// inflated internal length can trigger a huge allocation in
				// gnark's deserializer despite the outer size being correct.
				malicious := make([]byte, types.Groth16VkeySize)
				copy(malicious, groth16Vk[:288])
				malicious[288] = 0xFF
				malicious[289] = 0xFF
				malicious[290] = 0xFF
				malicious[291] = 0xFF
				msg.Groth16Vkey = malicious
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "492-byte vkey with inflated CommitmentKeys length is rejected",
			mallate: func() {
				malicious := make([]byte, types.Groth16VkeySize)
				copy(malicious, groth16Vk)
				// Set CommitmentKeys length at offset 484 to 0xFFFFFFFF.
				// Passes size and G1.K checks but would OOM gnark.
				malicious[484] = 0xFF
				malicious[485] = 0xFF
				malicious[486] = 0xFF
				malicious[487] = 0xFF
				msg.Groth16Vkey = malicious
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "492-byte vkey with inflated PublicAndCommitmentCommitted length is rejected",
			mallate: func() {
				malicious := make([]byte, types.Groth16VkeySize)
				copy(malicious, groth16Vk)
				// Set PublicAndCommitmentCommitted length at offset 488 to 0xFFFFFFFF.
				malicious[488] = 0xFF
				malicious[489] = 0xFF
				malicious[490] = 0xFF
				malicious[491] = 0xFF
				msg.Groth16Vkey = malicious
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
				Proof:        bytes.Repeat([]byte{0x01}, types.ProofBytesLen),
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
				msg.PublicValues = bytes.Repeat([]byte{0x01}, types.StateMembershipValuesBytes+1)
			},
			expErr: types.ErrInvalidPublicValuesLength,
		},
		{
			name: "public values too small",
			malleate: func() {
				msg.PublicValues = bytes.Repeat([]byte{0x01}, types.StateMembershipValuesBytes-1)
			},
			expErr: types.ErrInvalidPublicValuesLength,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg = &types.MsgSubmitMessages{
				Id:           util.CreateMockHexAddress("module", 1),
				Proof:        bytes.Repeat([]byte{0x01}, types.ProofBytesLen),
				PublicValues: bytes.Repeat([]byte{0x01}, types.StateMembershipValuesBytes),
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

func TestMsgUpdateVerifyingKeysValidateBasic(t *testing.T) {
	var msg *types.MsgUpdateVerifyingKeys

	groth16Vk := readGroth16Vkey(t)

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
			name: "invalid groth16 verifying key",
			malleate: func() {
				msg.Groth16Vkey = []byte{0x01}
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "invalid state transition verifying key length",
			malleate: func() {
				msg.StateTransitionVkey = []byte{0x01}
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "invalid state membership verifying key length",
			malleate: func() {
				msg.StateMembershipVkey = []byte{0x01}
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg = &types.MsgUpdateVerifyingKeys{
				Id:                  util.CreateMockHexAddress("module", 1),
				Owner:               "owner",
				Groth16Vkey:         groth16Vk,
				StateTransitionVkey: bytes.Repeat([]byte{0x01}, 32),
				StateMembershipVkey: bytes.Repeat([]byte{0x02}, 32),
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

func TestMsgTransferOwnershipValidateBasic(t *testing.T) {
	var msg *types.MsgTransferOwnership

	tests := []struct {
		name     string
		malleate func()
		expErr   error
	}{
		{
			name:     "success: transfer",
			malleate: func() {},
			expErr:   nil,
		},
		{
			name: "success: renounce",
			malleate: func() {
				msg.NewOwner = ""
				msg.RenounceOwnership = true
			},
			expErr: nil,
		},
		{
			name: "zero ism identifier",
			malleate: func() {
				msg.Id = util.NewZeroAddress()
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "neither new owner nor renounce",
			malleate: func() {
				msg.NewOwner = ""
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "new owner and renounce at the same time",
			malleate: func() {
				msg.RenounceOwnership = true
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid new owner address",
			malleate: func() {
				msg.NewOwner = "not-an-address"
			},
			expErr: sdkerrors.ErrInvalidAddress,
		},
	}

	// Encode the new owner with whatever bech32 prefix the sdk config currently holds.
	newOwner := sdk.AccAddress(bytes.Repeat([]byte{0x02}, 20)).String()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg = &types.MsgTransferOwnership{
				Id:       util.CreateMockHexAddress("module", 1),
				Owner:    "owner",
				NewOwner: newOwner,
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

func TestMsgUpdateRootHistorySizeValidateBasic(t *testing.T) {
	tests := []struct {
		name   string
		msg    types.MsgUpdateRootHistorySize
		expErr error
	}{
		{
			name: "success",
			msg:  types.MsgUpdateRootHistorySize{Id: util.CreateMockHexAddress("module", 1), RootHistorySize: types.MaxRootHistorySize},
		},
		{
			name: "success: zero restores the default",
			msg:  types.MsgUpdateRootHistorySize{Id: util.CreateMockHexAddress("module", 1)},
		},
		{
			name:   "zero ism identifier",
			msg:    types.MsgUpdateRootHistorySize{Id: util.NewZeroAddress(), RootHistorySize: 1},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name:   "root history size above the maximum",
			msg:    types.MsgUpdateRootHistorySize{Id: util.CreateMockHexAddress("module", 1), RootHistorySize: types.MaxRootHistorySize + 1},
			expErr: types.ErrInvalidRootHistorySize,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expErr != nil {
				require.ErrorIs(t, err, tc.expErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
