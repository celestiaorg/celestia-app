package keeper_test

import (
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
)

func (suite *KeeperTestSuite) TestInitGenesis() {
	isms := make([]types.InterchainSecurityModule, 0, 100)
	treeStates := make([]types.GenesisMerkleTreeState, 0, 50)
	messageProofSubmitted := make([]types.GenesisProofSubmission, 0, 50)
	for i := range 100 {
		ismId := util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), uint64(i))
		ism := types.InterchainSecurityModule{Id: ismId, Owner: "test"}

		isms = append(isms, ism)

		if i%2 == 0 {
			root := util.GenerateHexAddress([20]byte{0x02}, uint32(types.ModuleTypeZkISM), uint64(i))
			treeStates = append(treeStates, types.GenesisMerkleTreeState{
				Id:        ismId,
				TreeState: types.MerkleTreeState{Root: root.Bytes(), Count: uint32(i)},
			})
		}

		if i%3 == 0 {
			messageProofSubmitted = append(messageProofSubmitted, types.GenesisProofSubmission{
				Id:        ismId,
				Submitted: i%6 == 0,
			})
		}
	}

	genesisState := types.GenesisState{
		Isms:        isms,
		Submissions: messageProofSubmitted,
		TreeStates:  treeStates,
	}

	err := suite.zkISMKeeper.InitGenesis(suite.ctx, &genesisState)
	suite.Require().NoError(err)

	for _, ism := range genesisState.Isms {
		has, err := suite.zkISMKeeper.Exists(suite.ctx, ism.Id)
		suite.Require().NoError(err)
		suite.Require().True(has)
	}

	for _, entry := range genesisState.TreeStates {
		treeState, err := suite.zkISMKeeper.GetMerkleTreeState(suite.ctx, entry.Id)
		suite.Require().NoError(err)
		suite.Require().Equal(entry.TreeState, treeState)
	}

	for _, entry := range genesisState.Submissions {
		submitted, err := suite.zkISMKeeper.GetMessageProofSubmitted(suite.ctx, entry.Id)
		suite.Require().NoError(err)
		suite.Require().Equal(entry.Submitted, submitted)
	}
}

func (suite *KeeperTestSuite) TestInitGenesisTreeStateUnknownIsm() {
	genesisState := types.GenesisState{
		TreeStates: []types.GenesisMerkleTreeState{
			{
				Id:        util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), 0),
				TreeState: types.MerkleTreeState{Root: make([]byte, 32), Count: 1},
			},
		},
	}

	err := suite.zkISMKeeper.InitGenesis(suite.ctx, &genesisState)
	suite.Require().ErrorIs(err, types.ErrIsmNotFound)
}

func (suite *KeeperTestSuite) TestExportGenesis() {
	isms := make([]types.InterchainSecurityModule, 0, 100)
	for i := range 100 {
		ismId := util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), uint64(i))
		ism := types.InterchainSecurityModule{Id: ismId, Owner: "test"}

		err := suite.zkISMKeeper.SetIsm(suite.ctx, ismId, ism)
		suite.Require().NoError(err)

		isms = append(isms, ism)
	}

	genesisState, err := suite.zkISMKeeper.ExportGenesis(suite.ctx)
	suite.Require().NoError(err)
	suite.Require().Equal(isms, genesisState.Isms)
	suite.Require().Empty(genesisState.TreeStates)
	suite.Require().Empty(genesisState.Submissions)

	expectedTreeStates := make([]types.GenesisMerkleTreeState, 0, len(isms))
	for i, ism := range isms {
		root := util.GenerateHexAddress([20]byte{0x02}, uint32(types.ModuleTypeZkISM), uint64(i))
		treeState := types.MerkleTreeState{Root: root.Bytes(), Count: uint32(i)}
		err := suite.zkISMKeeper.SetMerkleTreeState(suite.ctx, ism.Id, treeState)
		suite.Require().NoError(err)

		expectedTreeStates = append(expectedTreeStates, types.GenesisMerkleTreeState{
			Id:        ism.Id,
			TreeState: treeState,
		})
	}

	genesisState, err = suite.zkISMKeeper.ExportGenesis(suite.ctx)
	suite.Require().NoError(err)
	suite.Require().Equal(isms, genesisState.Isms)
	suite.Require().ElementsMatch(expectedTreeStates, genesisState.TreeStates)

	expectedProofSubmitted := make([]types.GenesisProofSubmission, 0, len(isms)/2)
	for i, ism := range isms {
		if i%2 != 0 {
			continue
		}
		submitted := i%4 == 0
		err := suite.zkISMKeeper.SetMessageProofSubmitted(suite.ctx, ism.Id, submitted)
		suite.Require().NoError(err)

		expectedProofSubmitted = append(expectedProofSubmitted, types.GenesisProofSubmission{
			Id:        ism.Id,
			Submitted: submitted,
		})
	}

	genesisState, err = suite.zkISMKeeper.ExportGenesis(suite.ctx)
	suite.Require().NoError(err)
	suite.Require().ElementsMatch(expectedProofSubmitted, genesisState.Submissions)
}
