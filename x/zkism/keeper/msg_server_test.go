package keeper_test

import (
	"encoding/hex"
	"errors"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (suite *KeeperTestSuite) TestCreateInterchainSecurityModule() {
	var msg *types.MsgCreateInterchainSecurityModule

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
	}{
		{
			name: "success",
			setupTest: func() {
				msg = &types.MsgCreateInterchainSecurityModule{
					Creator:             testfactory.TestAccAddr,
					State:               randBytes(128),
					Groth16Vkey:         randBytes(32),
					StateTransitionVkey: randBytes(32),
					StateMembershipVkey: randBytes(32),
				}
			},
			expError: nil,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset state

			tc.setupTest()

			msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
			res, err := msgServer.CreateInterchainSecurityModule(suite.ctx, msg)

			if tc.expError != nil {
				suite.Require().Error(err)
				suite.Require().Nil(res)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestUpdateInterchainSecurityModule() {
	trustedState, err := hex.DecodeString("cba82bb5e1679482417a2f3e702afcfcac71d635b39983aea1ec90662c295b4fcd00000000000000306a35299603c1c3225e813951f37a3a527d516a17e124aa5f1765ccdcf0bd792e0000000000000000000000000000000000000000000000000000a8045f161bf468bf4d443964a68700cf76e215626e076e76d23bd1f4c3b31184b5822fd7b4df15d5ce9a")
	suite.Require().NoError(err)

	ism := suite.CreateTestIsm(trustedState)
	proofBz, pubValues := readStateTransitionProofData(suite.T())

	var msg *types.MsgUpdateInterchainSecurityModule

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
	}{
		{
			name: "success",
			setupTest: func() {
				msg = &types.MsgUpdateInterchainSecurityModule{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: pubValues,
				}
			},
			expError: nil,
		},
		{
			name: "ism not found",
			setupTest: func() {
				msg = &types.MsgUpdateInterchainSecurityModule{
					Id: util.HexAddress{},
				}
			},
			expError: types.ErrIsmNotFound,
		},
		{
			name: "failed to unmarshal public values",
			setupTest: func() {
				msg = &types.MsgUpdateInterchainSecurityModule{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: []byte("invalid"),
				}
			},
			expError: sdkerrors.ErrInvalidType,
		},
		{
			name: "trusted state mismatch",
			setupTest: func() {
				msg = &types.MsgUpdateInterchainSecurityModule{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: marshalStateTransitionValues(suite, pubValues, func(v *types.StateTransitionValues) { v.State[0] ^= 1 }),
				}
			},
			expError: types.ErrInvalidTrustedState,
		},
		{
			name: "invalid groth16 verifying key",
			setupTest: func() {
				ism.Groth16Vkey = []byte("invalid")
				suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))
				msg = &types.MsgUpdateInterchainSecurityModule{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: pubValues,
				}
			},
			expError: types.ErrInvalidVerifyingKey,
		},
		{
			name: "invalid proof",
			setupTest: func() {
				membershipProof, _ := readStateMembershipProofData(suite.T())
				msg = &types.MsgUpdateInterchainSecurityModule{
					Id:           ism.Id,
					Proof:        membershipProof,
					PublicValues: pubValues,
				}
			},
			expError: types.ErrInvalidProof,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset state
			ism = suite.CreateTestIsm(trustedState)
			suite.Require().NoError(suite.zkISMKeeper.SetMessageProofSubmitted(suite.ctx, ism.Id, true))

			tc.setupTest()
			before := suite.snapshotIsm(ism.Id)

			msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
			res, err := msgServer.UpdateInterchainSecurityModule(suite.ctx, msg)

			if tc.expError != nil {
				suite.Require().ErrorIs(err, tc.expError)
				suite.Require().Nil(res)
				suite.Require().Equal(before, suite.snapshotIsm(ism.Id))
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)

				publicValues := new(types.StateTransitionValues)
				suite.Require().NoError(publicValues.Unmarshal(pubValues))

				suite.Require().Equal(publicValues.NewState, res.State)
				suite.Require().False(suite.snapshotIsm(ism.Id).submitted)
			}
		})
	}
}

// TODO: regenerate x/zkism/internal/testdata/state_membership from the SP1 membership program once it outputs the
// merkle tree root and count, then remove this skip.
const skipStateMembershipFixtures = "state membership fixtures predate the merkle tree root public values"

// stateMembershipTrustedState is the trusted state that the state membership fixtures are bound to.
const stateMembershipTrustedState = "93feaeaf040d7b179d2d9dc8fc3900308fece8ec5b678816c583861a5ba9aa7da704000000000000548dde286a0ffb85381e5258af5ca042715f745c43ad0389b4834bf608695bf6d70000000000000000000000000000000000000000000000000000a8045f161bf468bf4d443964a68700cf76e215626e076e76d23bd1f4c3b31184b5822fd7b4df15d5ce9a"

func (suite *KeeperTestSuite) TestSubmitMessages() {
	trustedState, err := hex.DecodeString(stateMembershipTrustedState)
	suite.Require().NoError(err)

	ism := suite.CreateTestIsm(trustedState)
	// The proof does not verify against these public values, so every case below is rejected.
	proofBz, _ := readStateMembershipProofData(suite.T())

	var msg *types.MsgSubmitMessages

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
	}{
		{
			name: "ism not found",
			setupTest: func() {
				msg = &types.MsgSubmitMessages{
					Id: util.HexAddress{},
				}
			},
			expError: types.ErrIsmNotFound,
		},
		{
			name: "failed to unmarshal public values",
			setupTest: func() {
				msg = &types.MsgSubmitMessages{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: []byte("invalid"),
				}
			},
			expError: sdkerrors.ErrInvalidType,
		},
		{
			name: "duplicate submission rejected",
			setupTest: func() {
				suite.Require().NoError(suite.zkISMKeeper.SetMessageProofSubmitted(suite.ctx, ism.Id, true))
				msg = &types.MsgSubmitMessages{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: marshalStateMembershipValues(suite, ism, func(*types.StateMembershipValues) {}),
				}
			},
			expError: types.ErrMessageProofAlreadySubmitted,
		},
		{
			name: "state root mismatch",
			setupTest: func() {
				msg = &types.MsgSubmitMessages{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: marshalStateMembershipValues(suite, ism, func(v *types.StateMembershipValues) { v.StateRoot[0] ^= 1 }),
				}
			},
			expError: types.ErrInvalidStateRoot,
		},
		{
			name: "merkle tree address mismatch",
			setupTest: func() {
				msg = &types.MsgSubmitMessages{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: marshalStateMembershipValues(suite, ism, func(v *types.StateMembershipValues) { v.MerkleTreeAddress[31] ^= 1 }),
				}
			},
			expError: types.ErrInvalidMerkleTreeAddress,
		},
		{
			name: "invalid groth16 verifying key",
			setupTest: func() {
				ism.Groth16Vkey = []byte("invalid")
				suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))
				msg = &types.MsgSubmitMessages{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: marshalStateMembershipValues(suite, ism, func(*types.StateMembershipValues) {}),
				}
			},
			expError: types.ErrInvalidVerifyingKey,
		},
		{
			name: "invalid proof",
			setupTest: func() {
				msg = &types.MsgSubmitMessages{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: marshalStateMembershipValues(suite, ism, func(*types.StateMembershipValues) {}),
				}
			},
			expError: types.ErrInvalidProof,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset state
			ism = suite.CreateTestIsm(trustedState)

			tc.setupTest()
			before := suite.snapshotIsm(ism.Id)

			msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
			res, err := msgServer.SubmitMessages(suite.ctx, msg)

			suite.Require().ErrorIs(err, tc.expError)
			suite.Require().Nil(res)
			suite.Require().Equal(before, suite.snapshotIsm(ism.Id))
		})
	}
}

func (suite *KeeperTestSuite) TestSubmitMessagesSuccess() {
	suite.T().Skip(skipStateMembershipFixtures)

	trustedState, err := hex.DecodeString(stateMembershipTrustedState)
	suite.Require().NoError(err)

	ism := suite.CreateTestIsm(trustedState)
	proofBz, pubValues := readStateMembershipProofData(suite.T())

	var publicValues types.StateMembershipValues
	suite.Require().NoError(publicValues.Unmarshal(pubValues))

	msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
	res, err := msgServer.SubmitMessages(suite.ctx, &types.MsgSubmitMessages{
		Id:           ism.Id,
		Proof:        proofBz,
		PublicValues: pubValues,
	})
	suite.Require().NoError(err)
	suite.Require().Equal(types.EncodeHex(publicValues.StateRoot[:]), res.StateRoot)
	suite.Require().Equal(types.EncodeHex(publicValues.TreeRoot[:]), res.TreeRoot)
	suite.Require().Equal(publicValues.Count, res.Count)

	treeState, err := suite.zkISMKeeper.GetMerkleTreeState(suite.ctx, ism.Id)
	suite.Require().NoError(err)
	suite.Require().Equal(publicValues.TreeRoot[:], treeState.Root)
	suite.Require().Equal(publicValues.Count, treeState.Count)
}

func (suite *KeeperTestSuite) TestSubmitMessagesAfterUpdate() {
	suite.T().Skip(skipStateMembershipFixtures)

	// Test that after UpdateInterchainSecurityModule, a new SubmitMessages is allowed
	trustedState1, err := hex.DecodeString("cba82bb5e1679482417a2f3e702afcfcac71d635b39983aea1ec90662c295b4fcd00000000000000306a35299603c1c3225e813951f37a3a527d516a17e124aa5f1765ccdcf0bd792e0000000000000000000000000000000000000000000000000000a8045f161bf468bf4d443964a68700cf76e215626e076e76d23bd1f4c3b31184b5822fd7b4df15d5ce9a")
	suite.Require().NoError(err)

	ism := suite.CreateTestIsm(trustedState1)
	msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)

	// Update to new state
	updateProof, updatePubValues := readStateTransitionProofData(suite.T())
	updateMsg := &types.MsgUpdateInterchainSecurityModule{
		Id:           ism.Id,
		Proof:        updateProof,
		PublicValues: updatePubValues,
	}
	updateRes, err := msgServer.UpdateInterchainSecurityModule(suite.ctx, updateMsg)
	suite.Require().NoError(err)
	suite.Require().NotNil(updateRes)

	// Now SubmitMessages should work with the new state
	submitProof, submitPubValues := readStateMembershipProofData(suite.T())
	submitMsg := &types.MsgSubmitMessages{
		Id:           ism.Id,
		Proof:        submitProof,
		PublicValues: submitPubValues,
	}
	submitRes, err := msgServer.SubmitMessages(suite.ctx, submitMsg)
	suite.Require().NoError(err)
	suite.Require().NotNil(submitRes)

	// Second SubmitMessages should fail
	submitRes2, err := msgServer.SubmitMessages(suite.ctx, submitMsg)
	suite.Require().Error(err)
	suite.Require().ErrorIs(err, types.ErrMessageProofAlreadySubmitted)
	suite.Require().Nil(submitRes2)
}

// ismSnapshot holds the ISM state that a rejected message must not change.
type ismSnapshot struct {
	ism          types.InterchainSecurityModule
	hasSubmitted bool
	submitted    bool
	hasTreeState bool
	treeState    types.MerkleTreeState
}

// snapshotIsm reads the ISM, its submission flag and its merkle tree state.
func (suite *KeeperTestSuite) snapshotIsm(id util.HexAddress) ismSnapshot {
	ism, err := suite.zkISMKeeper.GetIsm(suite.ctx, id)
	suite.Require().NoError(err)

	submitted, err := suite.zkISMKeeper.GetMessageProofSubmitted(suite.ctx, id)
	hasSubmitted := !errors.Is(err, collections.ErrNotFound)
	if hasSubmitted {
		suite.Require().NoError(err)
	}

	treeState, err := suite.zkISMKeeper.GetMerkleTreeState(suite.ctx, id)
	hasTreeState := !errors.Is(err, collections.ErrNotFound)
	if hasTreeState {
		suite.Require().NoError(err)
	}

	return ismSnapshot{ism: ism, hasSubmitted: hasSubmitted, submitted: submitted, hasTreeState: hasTreeState, treeState: treeState}
}

// marshalStateTransitionValues decodes bz, applies modify and re-encodes it.
func marshalStateTransitionValues(suite *KeeperTestSuite, bz []byte, modify func(*types.StateTransitionValues)) []byte {
	var values types.StateTransitionValues
	suite.Require().NoError(values.Unmarshal(bz))
	modify(&values)

	out, err := values.Marshal()
	suite.Require().NoError(err)
	return out
}

// marshalStateMembershipValues builds public values matching the ism, applies modify and encodes them.
func marshalStateMembershipValues(suite *KeeperTestSuite, ism types.InterchainSecurityModule, modify func(*types.StateMembershipValues)) []byte {
	values := types.StateMembershipValues{
		TreeRoot: [32]byte{0x01},
		Count:    1,
	}
	copy(values.StateRoot[:], ism.State[:32])
	copy(values.MerkleTreeAddress[:], ism.MerkleTreeAddress)
	modify(&values)

	out, err := values.Marshal()
	suite.Require().NoError(err)
	return out
}

func (suite *KeeperTestSuite) TestUpdateVerifyingKeys() {
	var msg *types.MsgUpdateVerifyingKeys

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
	}{
		{
			name:      "success",
			setupTest: func() {},
			expError:  nil,
		},
		{
			name: "ism not found",
			setupTest: func() {
				msg.Id = util.CreateMockHexAddress("ism", 2)
			},
			expError: types.ErrIsmNotFound,
		},
		{
			name: "sender is not the owner",
			setupTest: func() {
				msg.Owner = "not-the-owner"
			},
			expError: sdkerrors.ErrUnauthorized,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset state
			ism := suite.CreateTestIsm([]byte("trusted_root_with_at_least_32_bytes"))
			ism.Owner = testfactory.TestAccAddr
			suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))
			suite.Require().NoError(suite.zkISMKeeper.SetMessageProofSubmitted(suite.ctx, ism.Id, true))

			treeState := types.MerkleTreeState{Root: randBytes(32), Count: 7}
			suite.Require().NoError(suite.zkISMKeeper.SetMerkleTreeState(suite.ctx, ism.Id, treeState))

			msg = &types.MsgUpdateVerifyingKeys{
				Id:                  ism.Id,
				Owner:               testfactory.TestAccAddr,
				Groth16Vkey:         ism.Groth16Vkey,
				StateTransitionVkey: randBytes(32),
				StateMembershipVkey: randBytes(32),
			}

			tc.setupTest()
			before := suite.snapshotIsm(ism.Id)

			msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
			res, err := msgServer.UpdateVerifyingKeys(suite.ctx, msg)

			if tc.expError != nil {
				suite.Require().ErrorIs(err, tc.expError)
				suite.Require().Nil(res)
				suite.Require().Equal(before, suite.snapshotIsm(ism.Id))
				return
			}

			suite.Require().NoError(err)
			suite.Require().NotNil(res)

			after := suite.snapshotIsm(ism.Id)
			suite.Require().Equal(msg.Groth16Vkey, after.ism.Groth16Vkey)
			suite.Require().Equal(msg.StateTransitionVkey, after.ism.StateTransitionVkey)
			suite.Require().Equal(msg.StateMembershipVkey, after.ism.StateMembershipVkey)
			suite.Require().Equal(ism.State, after.ism.State)
			suite.Require().Equal(ism.Owner, after.ism.Owner)
			suite.Require().False(after.submitted, "submission flag must be reset")
			suite.Require().Equal(treeState, after.treeState)
		})
	}
}
