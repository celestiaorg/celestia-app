package keeper_test

import (
	"math"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (suite *KeeperTestSuite) TestInitGenesis() {
	isms := make([]types.InterchainSecurityModule, 0, 100)
	treeStates := make([]types.GenesisMerkleTreeState, 0, 50)
	stateRootHistory := make([]types.GenesisRootHistoryEntry, 0, 68)
	treeStateHistory := make([]types.GenesisRootHistoryEntry, 0, 34)
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
			for seq := range uint64(2) {
				stateRootHistory = append(stateRootHistory, types.GenesisRootHistoryEntry{
					Id:       ismId,
					Sequence: seq,
					Entry:    types.RootHistoryEntry{Root: randBytes(32), Height: int64(seq + 1)},
				})
			}
			treeStateHistory = append(treeStateHistory, types.GenesisRootHistoryEntry{
				Id:       ismId,
				Sequence: 7,
				Entry:    types.RootHistoryEntry{Root: randBytes(32), Count: uint32(i), Height: 3},
			})
		}
	}

	genesisState := types.GenesisState{
		Isms:             isms,
		TreeStates:       treeStates,
		StateRootHistory: stateRootHistory,
		TreeStateHistory: treeStateHistory,
	}

	// Import after the recorded heights so they are kept unchanged.
	suite.ctx = suite.ctx.WithBlockHeight(10)
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

	// Export returns exactly what was imported.
	exported, err := suite.zkISMKeeper.ExportGenesis(suite.ctx)
	suite.Require().NoError(err)
	suite.Require().ElementsMatch(genesisState.StateRootHistory, exported.StateRootHistory)
	suite.Require().ElementsMatch(genesisState.TreeStateHistory, exported.TreeStateHistory)
}

func (suite *KeeperTestSuite) TestInitGenesisIgnoresSubmissions() {
	ismId := util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), 0)
	genesisState := types.GenesisState{
		Isms:        []types.InterchainSecurityModule{{Id: ismId, Owner: "test"}},
		Submissions: []types.GenesisProofSubmission{{Id: ismId, Submitted: true}}, //nolint:staticcheck // deprecated field must stay accepted
	}

	suite.Require().NoError(suite.zkISMKeeper.InitGenesis(suite.ctx, &genesisState))

	exported, err := suite.zkISMKeeper.ExportGenesis(suite.ctx)
	suite.Require().NoError(err)
	suite.Require().Empty(exported.Submissions) //nolint:staticcheck // deprecated field is never exported
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
	suite.Require().Empty(genesisState.StateRootHistory)
	suite.Require().Empty(genesisState.TreeStateHistory)

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
}

func (suite *KeeperTestSuite) TestInitGenesisRootHistoryUnknownIsm() {
	genesisState := types.GenesisState{
		StateRootHistory: []types.GenesisRootHistoryEntry{
			{
				Id:    util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), 0),
				Entry: types.RootHistoryEntry{Root: make([]byte, 32), Height: 1},
			},
		},
	}

	err := suite.zkISMKeeper.InitGenesis(suite.ctx, &genesisState)
	suite.Require().ErrorIs(err, types.ErrIsmNotFound)
}

// TestInitGenesisLowersFutureHistoryHeights checks that heights above the restart height do not block the first proof.
func (suite *KeeperTestSuite) TestInitGenesisLowersFutureHistoryHeights() {
	ismId := util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), 0)
	ism := types.InterchainSecurityModule{Id: ismId, Owner: "test"}
	root := randBytes(32)
	genesisState := types.GenesisState{
		Isms: []types.InterchainSecurityModule{ism},
		StateRootHistory: []types.GenesisRootHistoryEntry{
			{Id: ismId, Entry: types.RootHistoryEntry{Root: root, Height: 5_000_000}},
		},
	}

	suite.Require().NoError(suite.zkISMKeeper.InitGenesis(suite.ctx.WithBlockHeight(0), &genesisState))

	entries, err := suite.zkISMKeeper.GetStateRootHistory(suite.ctx, ismId)
	suite.Require().NoError(err)
	suite.Require().Equal([]types.RootHistoryEntry{{Root: root, Height: 0}}, entries)

	// The first replacement at the old height is recorded.
	replaced := randBytes(32)
	suite.Require().NoError(suite.zkISMKeeper.PushStateRootHistory(suite.ctx.WithBlockHeight(5_000_000), ism, replaced))
	entries, err = suite.zkISMKeeper.GetStateRootHistory(suite.ctx, ismId)
	suite.Require().NoError(err)
	suite.Require().Equal([][]byte{root, replaced}, historyRoots(entries))
}

func (suite *KeeperTestSuite) TestInitGenesisRootHistorySequenceBound() {
	ismId := util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), 0)
	newGenesis := func(sequence uint64) *types.GenesisState {
		return &types.GenesisState{
			Isms: []types.InterchainSecurityModule{{Id: ismId, Owner: "test"}},
			TreeStateHistory: []types.GenesisRootHistoryEntry{
				{Id: ismId, Sequence: sequence, Entry: types.RootHistoryEntry{Root: make([]byte, 32), Count: 1}},
			},
		}
	}

	suite.Require().ErrorIs(suite.zkISMKeeper.InitGenesis(suite.ctx, newGenesis(math.MaxInt64+1)), sdkerrors.ErrAppConfig)

	suite.SetupTest()
	suite.Require().NoError(suite.zkISMKeeper.InitGenesis(suite.ctx, newGenesis(math.MaxInt64)))
}

// TestInitGenesisLowersHistoryHeightsFromInitialHeight checks the clamp boundary for an initial height above 1.
func (suite *KeeperTestSuite) TestInitGenesisLowersHistoryHeightsFromInitialHeight() {
	ismId := util.GenerateHexAddress([20]byte{0x01}, uint32(types.ModuleTypeZkISM), 0)
	genesisState := types.GenesisState{
		Isms: []types.InterchainSecurityModule{{Id: ismId, Owner: "test", StateHeight: 10}},
		TreeStates: []types.GenesisMerkleTreeState{
			{Id: ismId, TreeState: types.MerkleTreeState{Root: randBytes(32), Count: 1, Height: 11}},
		},
		StateRootHistory: []types.GenesisRootHistoryEntry{
			{Id: ismId, Sequence: 0, Entry: types.RootHistoryEntry{Root: randBytes(32), Height: 9}},
			{Id: ismId, Sequence: 1, Entry: types.RootHistoryEntry{Root: randBytes(32), Height: 10}},
			{Id: ismId, Sequence: 2, Entry: types.RootHistoryEntry{Root: randBytes(32), Height: 11}},
		},
	}

	suite.Require().NoError(suite.zkISMKeeper.InitGenesis(suite.ctx.WithBlockHeight(10), &genesisState))

	entries, err := suite.zkISMKeeper.GetStateRootHistory(suite.ctx, ismId)
	suite.Require().NoError(err)
	heights := make([]int64, len(entries))
	for i, entry := range entries {
		heights[i] = entry.Height
	}
	suite.Require().Equal([]int64{9, 9, 9}, heights)

	// The ISM and tree state proof heights are lowered the same way.
	ism, err := suite.zkISMKeeper.GetIsm(suite.ctx, ismId)
	suite.Require().NoError(err)
	suite.Require().Equal(int64(9), ism.StateHeight)
	treeState, err := suite.zkISMKeeper.GetMerkleTreeState(suite.ctx, ismId)
	suite.Require().NoError(err)
	suite.Require().Equal(int64(9), treeState.Height)
}
