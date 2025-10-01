package keeper_test

import (
	"encoding/hex"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func (suite *KeeperTestSuite) TestCreateZKExecutionISM() {
	var msg *types.MsgCreateZKExecutionISM

	namespace, err := hex.DecodeString(namespaceHex)
	suite.Require().NoError(err)

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
	}{
		{
			name: "success",
			setupTest: func() {
				msg = &types.MsgCreateZKExecutionISM{
					Creator:             testfactory.TestAccAddr,
					Namespace:           namespace,
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
			res, err := msgServer.CreateZKExecutionISM(suite.ctx, msg)

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

func (suite *KeeperTestSuite) TestUpdateZKExecutionISM() {
	trustedRoot, err := hex.DecodeString("af50a407e7a9fcba29c46ad31e7690bae4e951e3810e5b898eda29d3d3e92dbe")
	suite.Require().NoError(err)

	ism := suite.CreateTestIsm(trustedRoot)
	proofBz, pubValues := readStateTransitionProofData(suite.T())

	var msg *types.MsgUpdateZKExecutionISM

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
	}{
		{
			name: "success",
			setupTest: func() {
				msg = &types.MsgUpdateZKExecutionISM{
					Id:           ism.Id,
					Height:       uint64(celestiaHeight),
					Proof:        proofBz,
					PublicValues: pubValues,
				}
			},
			expError: nil,
		},
		{
			name: "ism not found",
			setupTest: func() {
				msg = &types.MsgUpdateZKExecutionISM{
					Id: util.HexAddress{},
				}
			},
			expError: types.ErrIsmNotFound,
		},
		{
			name: "failed to unmarshal public values",
			setupTest: func() {
				msg = &types.MsgUpdateZKExecutionISM{
					Id:           ism.Id,
					Height:       uint64(celestiaHeight),
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
			res, err := msgServer.UpdateZKExecutionISM(suite.ctx, msg)

			if tc.expError != nil {
				suite.Require().Error(err)
				suite.Require().Nil(res)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)

				publicValues := new(types.EvExecutionPublicValues)
				suite.Require().NoError(publicValues.Unmarshal(pubValues))

				suite.Require().Equal(publicValues.NewHeight, res.Height)
				suite.Require().Equal(hex.EncodeToString(publicValues.NewStateRoot[:]), res.StateRoot)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestSubmitMessages() {
	trustedRoot, err := hex.DecodeString("acd4fcbcd3bbf25bd2055b2125f7d361f9f58d97ad167fe35a5b7f1806f5f8ea")
	suite.Require().NoError(err)

	ism := suite.CreateTestIsm(trustedRoot)
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

				publicValues := new(types.EvHyperlanePublicValues)
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

func (suite *KeeperTestSuite) TestUpdateParams() {
	var (
		expMaxHeaderHashes uint32 = 100
		msg                *types.MsgUpdateParams
	)

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
	}{
		{
			name: "success",
			setupTest: func() {
				msg = &types.MsgUpdateParams{
					Authority: authtypes.NewModuleAddress("gov").String(),
					Params:    types.NewParams(expMaxHeaderHashes),
				}
			},
			expError: nil,
		},
		{
			name: "unauthorized authority",
			setupTest: func() {
				msg = &types.MsgUpdateParams{
					Authority: "unauthorized",
					Params:    types.DefaultParams(),
				}
			},
			expError: sdkerrors.ErrUnauthorized,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset state

			tc.setupTest()

			msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
			res, err := msgServer.UpdateParams(suite.ctx, msg)

			if tc.expError != nil {
				suite.Require().Nil(res)
				suite.Require().ErrorIs(err, tc.expError)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)

				maxHeaderHashesParam, err := suite.zkISMKeeper.GetMaxHeaderHashes(suite.ctx)
				suite.Require().NoError(err)
				suite.Require().Equal(expMaxHeaderHashes, maxHeaderHashesParam)
			}
		})
	}
}
