package keeper_test

import (
	"encoding/binary"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestAliasIdsDoNotNameIsm checks that an id sharing an ISM's internal id but
// not its module specifier or module type does not name that ISM.
func (suite *KeeperTestSuite) TestAliasIdsDoNotNameIsm() {
	ism := suite.CreateTestIsm([]byte("trusted_root_with_at_least_32_bytes"))
	ism.Owner = testfactory.TestAccAddr
	suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))

	exists, err := suite.zkISMKeeper.Exists(suite.ctx, ism.Id)
	suite.Require().NoError(err)
	suite.Require().True(exists)

	otherType := ism.Id
	binary.BigEndian.PutUint32(otherType[20:24], 1)
	otherSpecifier := ism.Id
	otherSpecifier[0] ^= 0xff

	msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
	queryServer := keeper.NewQueryServerImpl(suite.zkISMKeeper)
	for _, alias := range []util.HexAddress{otherType, otherSpecifier} {
		suite.Require().Equal(ism.Id.GetInternalId(), alias.GetInternalId())

		exists, err := suite.zkISMKeeper.Exists(suite.ctx, alias)
		suite.Require().NoError(err)
		suite.Require().False(exists, "alias %s", alias)

		_, err = suite.zkISMKeeper.Verify(suite.ctx, alias, nil, util.HyperlaneMessage{})
		suite.Require().ErrorIs(err, types.ErrIsmNotFound)

		_, err = msgServer.TransferOwnership(suite.ctx, &types.MsgTransferOwnership{
			Id: alias, Owner: testfactory.TestAccAddr, RenounceOwnership: true,
		})
		suite.Require().ErrorIs(err, types.ErrIsmNotFound)

		_, err = queryServer.Ism(suite.ctx, &types.QueryIsmRequest{Id: alias.String()})
		suite.Require().Equal(codes.NotFound, status.Code(err))
	}

	stored, err := queryServer.Ism(suite.ctx, &types.QueryIsmRequest{Id: ism.Id.String()})
	suite.Require().NoError(err)
	suite.Require().Equal(testfactory.TestAccAddr, stored.Ism.Owner, "aliases must not change the ISM")
}
