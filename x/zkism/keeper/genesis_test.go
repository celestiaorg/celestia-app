package keeper_test

import (
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
)

func (suite *KeeperTestSuite) TestInitGenesis() {
	isms := make([]types.InterchainSecurityModule, 0, 100)
	for i := range 100 {
		ismId := util.GenerateHexAddress([20]byte{0x01}, types.ModuleTypeZkISM, uint64(i))
		ism := types.InterchainSecurityModule{Id: ismId, Owner: "test"}

		isms = append(isms, ism)
	}

	genesisState := types.GenesisState{
		Isms: isms,
	}

	err := suite.zkISMKeeper.InitGenesis(suite.ctx, &genesisState)
	suite.Require().NoError(err)

	for _, ism := range genesisState.Isms {
		has, err := suite.zkISMKeeper.Exists(suite.ctx, ism.Id)
		suite.Require().NoError(err)
		suite.Require().True(has)
	}
}

func (suite *KeeperTestSuite) TestExportGenesis() {
	isms := make([]types.InterchainSecurityModule, 0, 100)
	for i := range 100 {
		ismId := util.GenerateHexAddress([20]byte{0x01}, types.ModuleTypeZkISM, uint64(i))
		ism := types.InterchainSecurityModule{Id: ismId, Owner: "test"}

		err := suite.zkISMKeeper.SetIsm(suite.ctx, ismId, ism)
		suite.Require().NoError(err)

		isms = append(isms, ism)
	}

	genesisState, err := suite.zkISMKeeper.ExportGenesis(suite.ctx)
	suite.Require().NoError(err)
	suite.Require().Equal(isms, genesisState.Isms)
}
