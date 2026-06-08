package keeper_test

import (
	"encoding/hex"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v9/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v9/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v9/x/zkism/types"
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
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			tc.setupTest()

			msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
			res, err := msgServer.UpdateInterchainSecurityModule(suite.ctx, msg)

			if tc.expError != nil {
				suite.Require().Error(err)
				suite.Require().Nil(res)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)

				publicValues := new(types.StateTransitionValues)
				suite.Require().NoError(publicValues.Unmarshal(pubValues))

				suite.Require().Equal(publicValues.NewState, res.State)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestSubmitMessages() {
	trustedState, err := hex.DecodeString("93feaeaf040d7b179d2d9dc8fc3900308fece8ec5b678816c583861a5ba9aa7da704000000000000548dde286a0ffb85381e5258af5ca042715f745c43ad0389b4834bf608695bf6d70000000000000000000000000000000000000000000000000000a8045f161bf468bf4d443964a68700cf76e215626e076e76d23bd1f4c3b31184b5822fd7b4df15d5ce9a")
	suite.Require().NoError(err)

	ism := suite.CreateTestIsm(trustedState)
	proofBz, pubValues := readStateMembershipProofData(suite.T())

	var msg *types.MsgSubmitMessages

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
	}{
		{
			name: "success",
			setupTest: func() {
				msg = &types.MsgSubmitMessages{
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
				msg = &types.MsgSubmitMessages{
					Id:           ism.Id,
					Proof:        proofBz,
					PublicValues: pubValues,
				}
				// First submission should succeed
				msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
				_, err := msgServer.SubmitMessages(suite.ctx, msg)
				suite.Require().NoError(err)
			},
			expError: types.ErrMessageProofAlreadySubmitted,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset state
			ism = suite.CreateTestIsm(trustedState)

			tc.setupTest()

			msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
			res, err := msgServer.SubmitMessages(suite.ctx, msg)

			if tc.expError != nil {
				suite.Require().Error(err)
				suite.Require().Nil(res)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)

				publicValues := new(types.StateMembershipValues)
				suite.Require().NoError(publicValues.Unmarshal(pubValues))

				suite.Require().Equal(types.EncodeHex(publicValues.StateRoot[:]), res.StateRoot)

				for idx, id := range publicValues.MessageIds {
					has, err := suite.zkISMKeeper.HasMessageId(suite.ctx, ism.Id, id[:])
					suite.Require().NoError(err)
					suite.Require().True(has)

					suite.Require().Equal(types.EncodeHex(id[:]), res.Messages[idx])
				}
			}
		})
	}
}

func (suite *KeeperTestSuite) TestSubmitMessagesAfterUpdate() {
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
