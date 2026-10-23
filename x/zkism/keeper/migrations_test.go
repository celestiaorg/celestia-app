package keeper_test

import (
	"cosmossdk.io/collections"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/keeper"
)

func (suite *KeeperTestSuite) TestMigrate1to2() {
	ism := suite.CreateTestIsm([]byte("trusted_root"))

	suite.Require().NoError(suite.zkISMKeeper.SetMessageProofSubmitted(suite.ctx, ism.Id, true))

	err := keeper.NewMigrator(suite.zkISMKeeper).Migrate1to2(suite.ctx)
	suite.Require().NoError(err)

	_, err = suite.zkISMKeeper.GetMessageProofSubmitted(suite.ctx, ism.Id)
	suite.Require().ErrorIs(err, collections.ErrNotFound)

	stored, err := suite.zkISMKeeper.GetIsm(suite.ctx, ism.Id)
	suite.Require().NoError(err)
	suite.Require().Equal(ism, stored)

	_, err = suite.zkISMKeeper.GetMerkleTreeState(suite.ctx, ism.Id)
	suite.Require().ErrorIs(err, collections.ErrNotFound)
}
