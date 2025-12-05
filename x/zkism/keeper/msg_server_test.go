package keeper_test

import (
	"encoding/hex"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
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
					Height:       0,
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
					Height:       0,
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
			res, err := msgServer.SubmitMessages(suite.ctx, msg)

			if tc.expError != nil {
				suite.Require().Error(err)
				suite.Require().Nil(res)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)

				publicValues := new(types.StateMembershipValues)
				suite.Require().NoError(publicValues.Unmarshal(pubValues))

				for _, id := range publicValues.MessageIds {
					has, err := suite.zkISMKeeper.HasMessageId(suite.ctx, id[:])
					suite.Require().NoError(err)
					suite.Require().True(has)
				}
			}
		})
	}
}
