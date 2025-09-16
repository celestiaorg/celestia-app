package keeper_test

import (
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func (suite *KeeperTestSuite) TestCreateZKExecutionISM() {
	var msg *types.MsgCreateZKExecutionISM

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
