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
	trustedState, err := hex.DecodeString("75e7e4f02bf0ac0fedcaa2a99acbf948bed8643c5a7f28e6bfc37b9da099e970777c8f454c421f9f6402f2512a273c55078eadde2ac784288791d79f510ad56e2300000000000000770000000000000000000000000000000000000000000000000000a8045f161bf468bf4d443324b79a9a978e3de925c8069ad2316ee2324b3ca961d5941b5a68b2901c6ec9")
	suite.Require().NoError(err)

	trustedCelestiaHeight := uint64(36)
	trustedCelestiaHash, err := hex.DecodeString("573641f63ac8c7cb36a71918bdaaee5d6051704c05b4545241b46d77ba147d58")
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
	trustedState, err := hex.DecodeString("fac92413c55a229e0b67ca195c32f19cfab4ba670a150215302564cf68531d9fd2b39cf65fbbea4bdbf30aea8fc46e050b8fa020234ee8223acc25da3b26f4fb4200000000000000ef0000000000000000000000000000000000000000000000000000a8045f161bf468bf4d443324b79a9a978e3de925c8069ad2316ee2324b3ca961d5941b5a68b2901c6ec9")
	suite.Require().NoError(err)
	// trusted root is first 32 bytes of trusted state
	trustedRoot := trustedState[:32]
	suite.Require().NoError(err)
	trustedCelestiaHash, err := hex.DecodeString("777c8f454c421f9f6402f2512a273c55078eadde2ac784288791d79f510ad56e")
	suite.Require().NoError(err)
	trustedCelestiaHeight := uint64(35)

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
