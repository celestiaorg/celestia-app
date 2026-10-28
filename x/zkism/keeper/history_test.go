package keeper_test

import (
	"encoding/hex"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// pushStateRoots records one replaced state root per block, starting at startHeight, and returns the roots.
func (suite *KeeperTestSuite) pushStateRoots(ism types.InterchainSecurityModule, startHeight int64, n int) [][]byte {
	roots := make([][]byte, n)
	for i := range roots {
		roots[i] = randBytes(32)
		ctx := suite.ctx.WithBlockHeight(startHeight + int64(i))
		suite.Require().NoError(suite.zkISMKeeper.PushStateRootHistory(ctx, ism, roots[i]))
	}
	return roots
}

func historyRoots(entries []types.RootHistoryEntry) [][]byte {
	roots := make([][]byte, len(entries))
	for i, entry := range entries {
		roots[i] = entry.Root
	}
	return roots
}

func (suite *KeeperTestSuite) TestRootHistoryRecordsEveryPush() {
	ism := suite.CreateTestIsm(randBytes(64))

	// The message server admits one proof per ISM per block, so the history itself records every push it is given.
	ctx := suite.ctx.WithBlockHeight(10)
	roots := [][]byte{randBytes(32), randBytes(32)}
	for _, root := range roots {
		suite.Require().NoError(suite.zkISMKeeper.PushStateRootHistory(ctx, ism, root))
	}

	entries, err := suite.zkISMKeeper.GetStateRootHistory(suite.ctx, ism.Id)
	suite.Require().NoError(err)
	suite.Require().Equal(roots, historyRoots(entries))
	for _, entry := range entries {
		suite.Require().Equal(int64(10), entry.Height)
	}
}

func (suite *KeeperTestSuite) TestRootHistoryEvictsOldest() {
	ism := suite.CreateTestIsm(randBytes(64))
	ism.RootHistorySize = 3
	suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))

	roots := suite.pushStateRoots(ism, 1, 5)

	entries, err := suite.zkISMKeeper.GetStateRootHistory(suite.ctx, ism.Id)
	suite.Require().NoError(err)
	suite.Require().Equal(roots[2:], historyRoots(entries), "only the newest root history size roots remain, oldest first")
}

func (suite *KeeperTestSuite) TestRootHistoryIsPerIsm() {
	ism := suite.CreateTestIsm(randBytes(64))
	other := ism
	other.Id = util.CreateMockHexAddress("ism", 2)
	suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, other.Id, other))

	roots := suite.pushStateRoots(ism, 1, 2)
	otherRoots := suite.pushStateRoots(other, 1, 1)

	entries, err := suite.zkISMKeeper.GetStateRootHistory(suite.ctx, ism.Id)
	suite.Require().NoError(err)
	suite.Require().Equal(roots, historyRoots(entries))

	entries, err = suite.zkISMKeeper.GetStateRootHistory(suite.ctx, other.Id)
	suite.Require().NoError(err)
	suite.Require().Equal(otherRoots, historyRoots(entries))
}

// TestSubmitMessagesAgainstHistoricalStateRoot submits the real membership proof after the state root it was
// generated against has been replaced.
func (suite *KeeperTestSuite) TestSubmitMessagesAgainstHistoricalStateRoot() {
	provenState, err := hex.DecodeString(stateMembershipTrustedState)
	suite.Require().NoError(err)
	proofBz, pubValues := readStateMembershipProofData(suite.T())

	var publicValues types.StateMembershipValues
	suite.Require().NoError(publicValues.Unmarshal(pubValues))

	testCases := []struct {
		name        string
		historySize uint32
		// newerRoots is the number of state roots replaced, one per block, after the proven root.
		newerRoots int
		expError   error
	}{
		{
			name:        "success: proven root was just replaced",
			historySize: 2,
			newerRoots:  0,
		},
		{
			name:        "success: proven root is the oldest root still kept",
			historySize: 2,
			newerRoots:  1,
		},
		{
			name:        "proven root was evicted",
			historySize: 2,
			newerRoots:  2,
			expError:    types.ErrInvalidStateRoot,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			ism := suite.CreateTestIsm(randBytes(64))
			ism.RootHistorySize = tc.historySize
			suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))

			suite.Require().NoError(suite.zkISMKeeper.PushStateRootHistory(suite.ctx.WithBlockHeight(1), ism, provenState[:32]))
			suite.pushStateRoots(ism, 2, tc.newerRoots)

			msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
			res, err := msgServer.SubmitMessages(suite.ctx, &types.MsgSubmitMessages{Id: ism.Id, Proof: proofBz, PublicValues: pubValues})
			if tc.expError != nil {
				suite.Require().ErrorIs(err, tc.expError)
				return
			}

			suite.Require().NoError(err)
			suite.Require().Equal(types.EncodeHex(provenState[:32]), res.StateRoot)

			treeState, err := suite.zkISMKeeper.GetMerkleTreeState(suite.ctx, ism.Id)
			suite.Require().NoError(err)
			suite.Require().Equal(publicValues.TreeRoot[:], treeState.Root)
			suite.Require().Equal(publicValues.Count, treeState.Count)
		})
	}
}

// TestSubmitMessagesKeepsReplacedTreeState checks that a newer tree state moves the previous one into the history and
// that messages relayed with merkle proofs against the previous root still verify.
func (suite *KeeperTestSuite) TestSubmitMessagesKeepsReplacedTreeState() {
	provenState, err := hex.DecodeString(stateMembershipTrustedState)
	suite.Require().NoError(err)
	proofBz, pubValues := readStateMembershipProofData(suite.T())

	var publicValues types.StateMembershipValues
	suite.Require().NoError(publicValues.Unmarshal(pubValues))
	suite.Require().Greater(publicValues.Count, uint32(1))

	messages := readStateMembershipMessages(suite.T())
	leaves := make([][32]byte, len(messages))
	for i, message := range messages {
		leaves[i] = message.Id()
	}

	// The previous tree state holds all leaves but the last one.
	previousLeaves := leaves[:len(leaves)-1]
	tree := util.NewTree(util.ZeroHashes, 0)
	for _, leaf := range previousLeaves {
		suite.Require().NoError(tree.Insert(leaf))
	}
	previousRoot := tree.GetRoot()
	previous := types.MerkleTreeState{Root: previousRoot[:], Count: uint32(len(previousLeaves)), Height: suite.ctx.BlockHeight() - 1}

	ism := suite.CreateTestIsm(provenState)
	suite.Require().NoError(suite.zkISMKeeper.SetMerkleTreeState(suite.ctx, ism.Id, previous))

	msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
	_, err = msgServer.SubmitMessages(suite.ctx, &types.MsgSubmitMessages{Id: ism.Id, Proof: proofBz, PublicValues: pubValues})
	suite.Require().NoError(err)

	treeStates, err := suite.zkISMKeeper.GetTreeStateHistory(suite.ctx, ism.Id)
	suite.Require().NoError(err)
	suite.Require().Equal([]types.RootHistoryEntry{{Root: previous.Root, Count: previous.Count, Height: suite.ctx.BlockHeight()}}, treeStates)

	latest, err := suite.zkISMKeeper.GetMerkleTreeState(suite.ctx, ism.Id)
	suite.Require().NoError(err)
	suite.Require().Equal(suite.ctx.BlockHeight(), latest.Height)

	// A message relayed against the previous root and one relayed against the new root both verify.
	authorized, err := suite.zkISMKeeper.Verify(suite.ctx, ism.Id, merkleProofMetadata(previousLeaves, 0), messages[0])
	suite.Require().NoError(err)
	suite.Require().True(authorized)

	authorized, err = suite.zkISMKeeper.Verify(suite.ctx, ism.Id, merkleProofMetadata(leaves, 0), messages[0])
	suite.Require().NoError(err)
	suite.Require().True(authorized)
}

func (suite *KeeperTestSuite) TestVerifyAgainstTreeStateHistory() {
	leaves := make([][32]byte, 6)
	for i := range leaves {
		leaves[i] = util.HyperlaneMessage{Nonce: uint32(i)}.Id()
	}
	rootOf := func(n int) []byte {
		tree := util.NewTree(util.ZeroHashes, 0)
		for _, leaf := range leaves[:n] {
			suite.Require().NoError(tree.Insert(leaf))
		}
		root := tree.GetRoot()
		return root[:]
	}

	testCases := []struct {
		name       string
		history    []types.MerkleTreeState
		index      uint32
		proofOver  int
		authorized bool
	}{
		{
			name:       "success: proof against a replaced root",
			history:    []types.MerkleTreeState{{Root: rootOf(4), Count: 4}},
			index:      3,
			proofOver:  4,
			authorized: true,
		},
		{
			name:       "success: proof against the oldest of several replaced roots",
			history:    []types.MerkleTreeState{{Root: rootOf(3), Count: 3}, {Root: rootOf(4), Count: 4}},
			index:      1,
			proofOver:  3,
			authorized: true,
		},
		{
			name:       "replaced root not in the history",
			history:    []types.MerkleTreeState{{Root: rootOf(3), Count: 3}},
			index:      1,
			proofOver:  4,
			authorized: false,
		},
		{
			name:       "leaf index beyond the replaced root's count",
			history:    []types.MerkleTreeState{{Root: rootOf(4), Count: 3}},
			index:      3,
			proofOver:  4,
			authorized: false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			ism := suite.CreateTestIsm(randBytes(64))
			suite.Require().NoError(suite.zkISMKeeper.SetMerkleTreeState(suite.ctx, ism.Id, types.MerkleTreeState{Root: rootOf(6), Count: 6}))
			for i, treeState := range tc.history {
				suite.Require().NoError(suite.zkISMKeeper.PushTreeStateHistory(suite.ctx.WithBlockHeight(int64(i+1)), ism, treeState))
			}

			message := util.HyperlaneMessage{Nonce: tc.index}
			authorized, err := suite.zkISMKeeper.Verify(suite.ctx, ism.Id, merkleProofMetadata(leaves[:tc.proofOver], tc.index), message)
			suite.Require().NoError(err)
			suite.Require().Equal(tc.authorized, authorized)
		})
	}
}

func (suite *KeeperTestSuite) TestUpdateRootHistorySize() {
	var msg *types.MsgUpdateRootHistorySize

	testCases := []struct {
		name      string
		setupTest func()
		expError  error
		// expKept is the number of newest history entries kept out of the five pushed.
		expKept int
	}{
		{
			name:      "success: shrink drops the oldest entries",
			setupTest: func() { msg.RootHistorySize = 2 },
			expKept:   2,
		},
		{
			name:      "success: grow keeps every entry",
			setupTest: func() { msg.RootHistorySize = types.MaxRootHistorySize },
			expKept:   5,
		},
		{
			name:      "success: zero restores the default size",
			setupTest: func() { msg.RootHistorySize = 0 },
			expKept:   5,
		},
		{
			name:      "ism not found",
			setupTest: func() { msg.Id = util.CreateMockHexAddress("ism", 2) },
			expError:  types.ErrIsmNotFound,
		},
		{
			name:      "sender is not the owner",
			setupTest: func() { msg.Owner = "not-the-owner" },
			expError:  sdkerrors.ErrUnauthorized,
		},
		{
			name:      "size above the maximum",
			setupTest: func() { msg.RootHistorySize = types.MaxRootHistorySize + 1 },
			expError:  types.ErrInvalidRootHistorySize,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			ism := suite.CreateTestIsm(randBytes(64))
			ism.Owner = testfactory.TestAccAddr
			suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))

			stateRoots := suite.pushStateRoots(ism, 1, 5)
			for i := range 5 {
				treeState := types.MerkleTreeState{Root: randBytes(32), Count: uint32(i + 1)}
				suite.Require().NoError(suite.zkISMKeeper.PushTreeStateHistory(suite.ctx.WithBlockHeight(int64(i+1)), ism, treeState))
			}

			msg = &types.MsgUpdateRootHistorySize{Id: ism.Id, Owner: testfactory.TestAccAddr, RootHistorySize: 2}
			tc.setupTest()
			before := suite.snapshotIsm(ism.Id)

			msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
			res, err := msgServer.UpdateRootHistorySize(suite.ctx, msg)

			if tc.expError != nil {
				suite.Require().ErrorIs(err, tc.expError)
				suite.Require().Nil(res)
				suite.Require().Equal(before, suite.snapshotIsm(ism.Id))
				return
			}

			suite.Require().NoError(err)
			after := suite.snapshotIsm(ism.Id)
			suite.Require().Equal(msg.RootHistorySize, after.ism.RootHistorySize)
			suite.Require().Equal(stateRoots[5-tc.expKept:], historyRoots(after.stateRoots))
			suite.Require().Len(after.treeStates, tc.expKept)
			suite.Require().Equal(uint32(5), after.treeStates[tc.expKept-1].Count, "the newest tree state is kept")
		})
	}
}

func (suite *KeeperTestSuite) TestUpdateRootHistorySizeRenounced() {
	ism := suite.CreateTestIsm(randBytes(64))
	ism.Owner = ""
	suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))

	msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
	for _, owner := range []string{"", testfactory.TestAccAddr} {
		_, err := msgServer.UpdateRootHistorySize(suite.ctx, &types.MsgUpdateRootHistorySize{Id: ism.Id, Owner: owner, RootHistorySize: 1})
		suite.Require().ErrorIs(err, sdkerrors.ErrUnauthorized, "owner %q", owner)
	}
}

func (suite *KeeperTestSuite) TestQueryRootHistory() {
	ism := suite.CreateTestIsm(randBytes(64))
	stateRoots := suite.pushStateRoots(ism, 1, 2)
	treeState := types.MerkleTreeState{Root: randBytes(32), Count: 9}
	suite.Require().NoError(suite.zkISMKeeper.PushTreeStateHistory(suite.ctx.WithBlockHeight(3), ism, treeState))

	queryServer := keeper.NewQueryServerImpl(suite.zkISMKeeper)
	res, err := queryServer.RootHistory(suite.ctx, &types.QueryRootHistoryRequest{Id: ism.Id.String()})
	suite.Require().NoError(err)
	suite.Require().Equal(uint32(types.DefaultRootHistorySize), res.RootHistorySize, "an unset size reports the default")
	suite.Require().Equal(stateRoots, historyRoots(res.StateRoots))
	suite.Require().Equal([]types.RootHistoryEntry{{Root: treeState.Root, Count: treeState.Count, Height: 3}}, res.TreeStates)

	_, err = queryServer.RootHistory(suite.ctx, &types.QueryRootHistoryRequest{Id: util.CreateMockHexAddress("ism", 2).String()})
	suite.Require().Error(err)
}
