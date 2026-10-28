package types_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
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
			name: "groth16 vkey with inflated G1.K length is rejected before deserialization",
			malleate: func(gs *types.GenesisState) {
				malicious := make([]byte, 292)
				copy(malicious, groth16Vk[:288])
				malicious[288] = 0xFF
				malicious[289] = 0xFF
				malicious[290] = 0xFF
				malicious[291] = 0xFF
				gs.Isms[0].Groth16Vkey = malicious
			},
			expErr: types.ErrInvalidVerifyingKey,
		},
		{
			name: "492-byte vkey with inflated G1.K length bypasses size check",
			malleate: func(gs *types.GenesisState) {
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
				gs.Isms[0].Groth16Vkey = malicious
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
			name: "merkle tree state zero identifier",
			malleate: func(gs *types.GenesisState) {
				gs.TreeStates[0].Id = util.NewZeroAddress()
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "merkle tree state for unknown ism",
			malleate: func(gs *types.GenesisState) {
				gs.TreeStates[0].Id = util.CreateMockHexAddress("module", 2)
			},
			expErr: types.ErrIsmNotFound,
		},
		{
			name: "duplicate merkle tree state entry",
			malleate: func(gs *types.GenesisState) {
				gs.TreeStates = append(gs.TreeStates, gs.TreeStates[0])
			},
			expErr: sdkerrors.ErrAppConfig,
		},
		{
			name: "invalid merkle tree root length",
			malleate: func(gs *types.GenesisState) {
				gs.TreeStates[0].TreeState.Root = bytes.Repeat([]byte{0x01}, 31)
			},
			expErr: sdkerrors.ErrAppConfig,
		},
		{
			name: "root history size above the maximum",
			malleate: func(gs *types.GenesisState) {
				gs.Isms[0].RootHistorySize = types.MaxRootHistorySize + 1
			},
			expErr: types.ErrInvalidRootHistorySize,
		},
		{
			name: "state root history zero identifier",
			malleate: func(gs *types.GenesisState) {
				gs.StateRootHistory[0].Id = util.NewZeroAddress()
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "state root history for unknown ism",
			malleate: func(gs *types.GenesisState) {
				gs.StateRootHistory[0].Id = util.CreateMockHexAddress("module", 2)
			},
			expErr: types.ErrIsmNotFound,
		},
		{
			name: "duplicate state root history sequence",
			malleate: func(gs *types.GenesisState) {
				gs.StateRootHistory = append(gs.StateRootHistory, gs.StateRootHistory[0])
			},
			expErr: sdkerrors.ErrAppConfig,
		},
		{
			name: "invalid state root history root length",
			malleate: func(gs *types.GenesisState) {
				gs.StateRootHistory[0].Entry.Root = bytes.Repeat([]byte{0x01}, 31)
			},
			expErr: sdkerrors.ErrAppConfig,
		},
		{
			name: "state root history sequence that could wrap around",
			malleate: func(gs *types.GenesisState) {
				gs.StateRootHistory[0].Sequence = math.MaxUint64
			},
			expErr: sdkerrors.ErrAppConfig,
		},
		{
			name: "success: largest state root history sequence",
			malleate: func(gs *types.GenesisState) {
				gs.StateRootHistory[0].Sequence = math.MaxInt64
			},
		},
		{
			name: "negative state root history height",
			malleate: func(gs *types.GenesisState) {
				gs.StateRootHistory[0].Entry.Height = -1
			},
			expErr: sdkerrors.ErrAppConfig,
		},
		{
			name: "merkle tree state history for unknown ism",
			malleate: func(gs *types.GenesisState) {
				gs.TreeStateHistory[0].Id = util.CreateMockHexAddress("module", 2)
			},
			expErr: types.ErrIsmNotFound,
		},
		{
			name: "merkle tree state history exceeds the root history size",
			malleate: func(gs *types.GenesisState) {
				gs.Isms[0].RootHistorySize = 1
				entry := gs.TreeStateHistory[0]
				entry.Sequence++
				gs.TreeStateHistory = append(gs.TreeStateHistory, entry)
			},
			expErr: sdkerrors.ErrAppConfig,
		},
		{
			name: "deprecated submissions are ignored",
			malleate: func(gs *types.GenesisState) {
				gs.Submissions = []types.GenesisProofSubmission{{Id: util.CreateMockHexAddress("module", 2), Submitted: true}} //nolint:staticcheck // deprecated field must stay accepted
			},
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
		TreeStates: []types.GenesisMerkleTreeState{
			{
				Id: ismID,
				TreeState: types.MerkleTreeState{
					Root:  bytes.Repeat([]byte{0x05}, 32),
					Count: 1,
				},
			},
		},
		StateRootHistory: []types.GenesisRootHistoryEntry{
			{Id: ismID, Sequence: 0, Entry: types.RootHistoryEntry{Root: bytes.Repeat([]byte{0x06}, 32), Height: 1}},
		},
		TreeStateHistory: []types.GenesisRootHistoryEntry{
			{Id: ismID, Sequence: 0, Entry: types.RootHistoryEntry{Root: bytes.Repeat([]byte{0x07}, 32), Count: 1, Height: 1}},
		},
	}
}
