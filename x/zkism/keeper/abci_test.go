package keeper_test

import (
	"cosmossdk.io/collections"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
)

func (suite *KeeperTestSuite) TestStoreHeaderHash() {
	var (
		insertHeight int64  = 150000
		pruneHeight  uint32 = 100000
	)

	for i := range types.DefaultMaxHeaderHashes {
		err := suite.zkISMKeeper.SetHeaderHash(suite.ctx, uint64(pruneHeight+i), randBytes(32))
		suite.Require().NoError(err)
	}

	ctx := suite.ctx.WithBlockHeight(insertHeight).WithHeaderHash([]byte("newHeaderHash"))

	err := suite.zkISMKeeper.StoreHeaderHash(ctx)
	suite.Require().NoError(err)

	headerHash, err := suite.zkISMKeeper.GetHeaderHash(ctx, uint64(pruneHeight))
	suite.Require().Nil(headerHash)
	suite.Require().ErrorIs(err, collections.ErrNotFound)

	headerHash, err = suite.zkISMKeeper.GetHeaderHash(ctx, 150000)
	suite.Require().NoError(err)
	suite.Require().Equal([]byte("newHeaderHash"), headerHash)
}

func (suite *KeeperTestSuite) TestStoreHeaderHashPrunesMultiple() {
	var (
		insertHeight int64  = 150000
		pruneHeight  uint32 = 100000
	)

	for i := range insertHeight {
		err := suite.zkISMKeeper.SetHeaderHash(suite.ctx, uint64(i), randBytes(32))
		suite.Require().NoError(err)
	}

	ctx := suite.ctx.WithBlockHeight(insertHeight).WithHeaderHash([]byte("newHeaderHash"))

	err := suite.zkISMKeeper.StoreHeaderHash(ctx)
	suite.Require().NoError(err)

	for i := 0; i <= int(pruneHeight); i++ {
		headerHash, err := suite.zkISMKeeper.GetHeaderHash(ctx, uint64(pruneHeight))
		suite.Require().Nil(headerHash)
		suite.Require().ErrorIs(err, collections.ErrNotFound)
	}

	headerHash, err := suite.zkISMKeeper.GetHeaderHash(ctx, 150000)
	suite.Require().NoError(err)
	suite.Require().Equal([]byte("newHeaderHash"), headerHash)
}
