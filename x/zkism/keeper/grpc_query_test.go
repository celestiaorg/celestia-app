package keeper_test

import (
	"bytes"
	"errors"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

func (suite *KeeperTestSuite) TestQueryServerIsm() {
	ismId := util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), 1)
	expIsm := types.InterchainSecurityModule{Owner: "test"}

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
			name:     "request cannot be empty",
			req:      nil,
			expError: errors.New("request cannot be empty"),
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
				Id: util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), 9999).String(),
			},
			expError: errors.New("ism not found"),
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
				suite.Require().Equal(expIsm, res.Ism)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryServerMerkleTreeState() {
	var (
		req       *types.QueryMerkleTreeStateRequest
		expResult types.MerkleTreeState
	)

	testCases := []struct {
		name      string
		setupTest func()
		expErr    error
	}{
		{
			name: "success",
			setupTest: func() {
				ismID := util.CreateMockHexAddress("ism", 1)
				req = &types.QueryMerkleTreeStateRequest{Id: ismID.String()}

				expResult = types.MerkleTreeState{Root: bytes.Repeat([]byte{0x01}, 32), Count: 5}
				err := suite.zkISMKeeper.SetMerkleTreeState(suite.ctx, ismID, expResult)
				suite.Require().NoError(err)
			},
			expErr: nil,
		},
		{
			name: "merkle tree state not found",
			setupTest: func() {
				req = &types.QueryMerkleTreeStateRequest{Id: util.CreateMockHexAddress("ism", 3).String()}
			},
			expErr: errors.New("merkle tree state not found"),
		},
		{
			name: "request cannot be empty",
			setupTest: func() {
				req = nil
			},
			expErr: errors.New("request cannot be empty"),
		},
		{
			name: "invalid ism id",
			setupTest: func() {
				req = &types.QueryMerkleTreeStateRequest{Id: "invalid"}
			},
			expErr: errors.New("invalid hex address"),
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()

			tc.setupTest()

			queryServer := keeper.NewQueryServerImpl(suite.zkISMKeeper)
			res, err := queryServer.MerkleTreeState(suite.ctx, req)

			if tc.expErr != nil {
				suite.Require().Error(err)
				suite.Require().ErrorContains(err, tc.expErr.Error())
				suite.Require().Nil(res)
			} else {
				suite.Require().NoError(err)
				suite.Require().Equal(expResult, res.TreeState)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryServerIsms() {
	var (
		expIsms []types.InterchainSecurityModule
		req     *types.QueryIsmsRequest
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
					ismId := util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), uint64(i))
					ism := types.InterchainSecurityModule{Owner: "test"}

					err := suite.zkISMKeeper.SetIsm(suite.ctx, ismId, ism)
					suite.Require().NoError(err)

					expIsms = append(expIsms, ism)
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
					ismId := util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), uint64(i))
					ism := types.InterchainSecurityModule{Owner: "test"}

					err := suite.zkISMKeeper.SetIsm(suite.ctx, ismId, ism)
					suite.Require().NoError(err)

					expIsms = append(expIsms, ism)
				}

				expIsms = expIsms[:10]
			},
			expError: nil,
		},
		{
			name: "zero isms in store",
			setupTest: func() {
				req = &types.QueryIsmsRequest{}
				expIsms = nil
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
				suite.Require().Equal(expIsms, res.Isms)
			}
		})
	}
}
