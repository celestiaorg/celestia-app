package keeper_test

import (
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
)

func (suite *KeeperTestSuite) TestInitGenesis() {
	isms := make([]types.InterchainSecurityModule, 0, 100)
	genesisMessages := make([]types.GenesisMessages, 0, 50)
	for i := range 100 {
		ismId := util.GenerateHexAddress([20]byte{0x01}, types.ModuleTypeZkISM, uint64(i))
		ism := types.InterchainSecurityModule{Id: ismId, Owner: "test"}

		isms = append(isms, ism)

		if i%2 == 0 {
			msgId := util.GenerateHexAddress([20]byte{0x02}, types.ModuleTypeZkISM, uint64(i))
			genesisMessages = append(genesisMessages, types.GenesisMessages{
				Id:       ismId,
				Messages: []string{msgId.String()},
			})
		}
	}

	genesisState := types.GenesisState{
		Isms:     isms,
		Messages: genesisMessages,
	}

	err := suite.zkISMKeeper.InitGenesis(suite.ctx, &genesisState)
	suite.Require().NoError(err)

	for _, ism := range genesisState.Isms {
		has, err := suite.zkISMKeeper.Exists(suite.ctx, ism.Id)
		suite.Require().NoError(err)
		suite.Require().True(has)
	}

	for _, message := range genesisState.Messages {
		for _, msgId := range message.Messages {
			decodedMsgId, err := util.DecodeHexAddress(msgId)
			suite.Require().NoError(err)

			has, err := suite.zkISMKeeper.HasMessageId(suite.ctx, message.Id, decodedMsgId.Bytes())
			suite.Require().NoError(err)
			suite.Require().True(has)
		}
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
	suite.Require().Empty(genesisState.Messages)

	expectedMessages := make([]types.GenesisMessages, 0, len(isms))
	for i, ism := range isms {
		msgId := util.GenerateHexAddress([20]byte{0x02}, types.ModuleTypeZkISM, uint64(i))
		err := suite.zkISMKeeper.SetMessageId(suite.ctx, ism.Id, msgId.Bytes())
		suite.Require().NoError(err)

		expectedMessages = append(expectedMessages, types.GenesisMessages{
			Id:       ism.Id,
			Messages: []string{msgId.String()},
		})
	}

	genesisState, err = suite.zkISMKeeper.ExportGenesis(suite.ctx)
	suite.Require().NoError(err)
	suite.Require().Equal(isms, genesisState.Isms)
	suite.Require().ElementsMatch(expectedMessages, genesisState.Messages)
}
