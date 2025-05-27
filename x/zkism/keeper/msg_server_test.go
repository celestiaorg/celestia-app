package keeper_test

import (
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v4/x/zkism/types"
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
					Creator:                    testfactory.TestAccAddr,
					StateTransitionVerifierKey: randBytes(32),
					StateMembershipVerifierKey: randBytes(32),
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
