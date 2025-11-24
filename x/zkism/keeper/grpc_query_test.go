package keeper_test

import (
	"errors"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

func (suite *KeeperTestSuite) TestQueryServerIsm() {
	ismId := util.GenerateHexAddress([20]byte{0x01}, types.InterchainSecurityModuleTypeZKExecution, 1)
	expIsm := types.EvolveEvmISM{Id: ismId, Owner: "test"}

	err := suite.zkISMKeeper.SetIsm(suite.ctx, ismId, expIsm)
	suite.Require().NoError(err)

	testCases := []struct {
		name     string
		req      *types.QueryIsmRequest
		expError error
	}{
		{
			name: "success",
			req: &types.QueryIsmRequest{
				Id: ismId.String(),
			},
			expError: nil,
		},
		{
			name: "invalid hex address",
			req: &types.QueryIsmRequest{
				Id: "invalid-hex",
			},
			expError: errors.New("invalid hex address"),
		},
		{
			name: "ism not found",
			req: &types.QueryIsmRequest{
				Id: util.GenerateHexAddress([20]byte{0x01}, types.InterchainSecurityModuleTypeZKExecution, 9999).String(),
			},
			expError: errors.New("not found"),
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			queryServer := keeper.NewQueryServerImpl(suite.zkISMKeeper)

			res, err := queryServer.Ism(suite.ctx, tc.req)

			if tc.expError != nil {
				suite.Require().Nil(res)
				suite.Require().Error(err)
				suite.Require().ErrorContains(err, tc.expError.Error())
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res.GetEvolveEvmIsm())
				suite.Require().Equal(&expIsm, res.GetEvolveEvmIsm())
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryServerIsms() {
	var (
		expIsmIds []string
		req       *types.QueryIsmsRequest
	)

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
	}{
		{
			name: "success",
			setupTest: func() {
				req = &types.QueryIsmsRequest{}

				for i := range 100 {
					ismId := util.GenerateHexAddress([20]byte{0x01}, types.InterchainSecurityModuleTypeZKExecution, uint64(i))
					ism := types.EvolveEvmISM{Id: ismId, Owner: "test"}

					err := suite.zkISMKeeper.SetIsm(suite.ctx, ismId, ism)
					suite.Require().NoError(err)

					expIsmIds = append(expIsmIds, ismId.String())
				}
			},
			expError: nil,
		},
		{
			name: "success: paginated",
			setupTest: func() {
				req = &types.QueryIsmsRequest{
					Pagination: &query.PageRequest{
						Limit: 10,
					},
				}

				for i := range 100 {
					ismId := util.GenerateHexAddress([20]byte{0x01}, types.InterchainSecurityModuleTypeZKExecution, uint64(i))
					ism := types.EvolveEvmISM{Id: ismId, Owner: "test"}

					err := suite.zkISMKeeper.SetIsm(suite.ctx, ismId, ism)
					suite.Require().NoError(err)

					expIsmIds = append(expIsmIds, ismId.String())
				}

				expIsmIds = expIsmIds[:10]
			},
			expError: nil,
		},
		{
			name: "zero isms in store",
			setupTest: func() {
				req = &types.QueryIsmsRequest{}
				expIsmIds = nil
			},
			expError: nil,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset the state entries

			tc.setupTest()

			queryServer := keeper.NewQueryServerImpl(suite.zkISMKeeper)
			res, err := queryServer.Isms(suite.ctx, req)

			if tc.expError != nil {
				suite.Require().Nil(res)
				suite.Require().Error(err)
				suite.Require().ErrorContains(err, tc.expError.Error())
			} else {
				suite.Require().NoError(err)
				suite.Require().Len(res.IsmIds, len(expIsmIds))
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryServerParams() {
	var (
		expParams types.Params
		req       *types.QueryParamsRequest
	)

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
	}{
		{
			name: "success",
			setupTest: func() {
				req = &types.QueryParamsRequest{}

				expParams = types.DefaultParams()
			},
			expError: nil,
		},
		{
			name: "request cannot be empty",
			setupTest: func() {
				req = nil
			},
			expError: errors.New("request cannot be empty"),
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			tc.setupTest()

			queryServer := keeper.NewQueryServerImpl(suite.zkISMKeeper)
			res, err := queryServer.Params(suite.ctx, req)

			if tc.expError != nil {
				suite.Require().Nil(res)
				suite.Require().Error(err)
				suite.Require().ErrorContains(err, tc.expError.Error())
			} else {
				suite.Require().NoError(err)
				suite.Require().Equal(expParams, res.Params)
			}
		})
	}
}
