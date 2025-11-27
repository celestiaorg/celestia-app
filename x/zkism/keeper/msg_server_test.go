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
	trustedState, err := hex.DecodeString("8d29024fff03736c34ed9d8fd28e4c3efccd327a67808f68a868cc4691dd9435287fbab01019e1e327b8020ba833c2a8661cc4d1654340ad1b2b17d3af692b802400000000000000770000000000000000000000000000000000000000000000000000a8045f161bf468bf4d44825a3f4ccd1d0a13a418683c03108460623b760888c34c81fc75d6af376839bd")
	suite.Require().NoError(err)

	trustedCelestiaHeight := uint64(36)
	trustedCelestiaHash, err := hex.DecodeString("287fbab01019e1e327b8020ba833c2a8661cc4d1654340ad1b2b17d3af692b80")
	suite.Require().NoError(err)

	ism := suite.CreateTestIsm(trustedState, trustedCelestiaHash, trustedCelestiaHeight)
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

				publicValues := new(types.PublicValues)
				suite.Require().NoError(publicValues.Unmarshal(pubValues))

				suite.Require().Equal(publicValues.NewState, res.State)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestSubmitMessages() {
	trustedRoot, err := hex.DecodeString("acd4fcbcd3bbf25bd2055b2125f7d361f9f58d97ad167fe35a5b7f1806f5f8ea")
	suite.Require().NoError(err)
	trustedCelestiaHash, err := hex.DecodeString("0a02e7b488766f5ba73f8b44d96e97e27ca61580050e4a798bb664216876aa44")
	suite.Require().NoError(err)
	trustedCelestiaHeight := uint64(29)

	ism := suite.CreateTestIsm(trustedRoot, trustedCelestiaHash, trustedCelestiaHeight)
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
