package keeper_test

import (
	"encoding/hex"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v7/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v7/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v7/x/zkism/types"
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
	trustedState, err := hex.DecodeString("fb5c60a71772493fc32293c8047a099aa0548aee4b15e1ee0455f26fb1d76b027500000000000000f3a7136c5a71726713acae63bdcee751f388d911021f3acf33d44322e63f18c3220000000000000000000000000000000000000000000000000000a8045f161bf468bf4d4411cc010a975cdd8e50850a6142fc4459071c8132cef8d3c9b547277ef793af2c")
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
	trustedState, err := hex.DecodeString("b1d302256aee21b0d2dc21d88612061d1c7bb5bd5a222d98bd29482e6ea33d33d20000000000000069652b91fa76676373f699801562df433d3a8799659e4ca86dbd10c927e343393a0000000000000000000000000000000000000000000000000000a8045f161bf468bf4d4411cc010a975cdd8e50850a6142fc4459071c8132cef8d3c9b547277ef793af2c")
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
	trustedState1, err := hex.DecodeString("fb5c60a71772493fc32293c8047a099aa0548aee4b15e1ee0455f26fb1d76b027500000000000000f3a7136c5a71726713acae63bdcee751f388d911021f3acf33d44322e63f18c3220000000000000000000000000000000000000000000000000000a8045f161bf468bf4d4411cc010a975cdd8e50850a6142fc4459071c8132cef8d3c9b547277ef793af2c")
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
