package keeper_test

import (
	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

// TestStaleMessagesSurviveStateRootTransition reproduces the vulnerability
// described in CELESTIA-199: messages authorized under state root R1 remain
// consumable via Verify() after the ISM transitions to state root R2.
//
// This happens because:
//  1. The message storage key is (ismId, messageId) with no state root component.
//  2. UpdateISM resets the submissions flag but never clears the messages collection.
//  3. Verify() checks messages.Has(ismId, messageId) without comparing against
//     the ISM's current state root.
func (suite *KeeperTestSuite) TestStaleMessagesSurviveStateRootTransition() {
	// Create an ISM with state root R1 (first 32 bytes of the state).
	stateR1 := make([]byte, 64)
	copy(stateR1[:32], []byte("state_root_R1___________________")) // 32 bytes
	ism := suite.CreateTestIsm(stateR1)

	// Simulate SubmitMessages: authorize 3 messages under state root R1.
	messages := []util.HyperlaneMessage{
		{Nonce: 100},
		{Nonce: 200},
		{Nonce: 300},
	}
	for _, msg := range messages {
		err := suite.zkISMKeeper.SetMessageId(suite.ctx, ism.Id, msg.Id().Bytes())
		suite.Require().NoError(err)
	}

	// Consume message[0] via Verify — should succeed and remove it.
	authorized, err := suite.zkISMKeeper.Verify(suite.ctx, ism.Id, nil, messages[0])
	suite.Require().NoError(err)
	suite.Require().True(authorized, "message[0] should be authorized under R1")

	// Simulate UpdateISM: transition state root R1 → R2.
	// In production this goes through ZK proof verification, but the bug is in
	// the storage layer — UpdateISM resets submissions but never clears messages.
	stateR2 := make([]byte, 64)
	copy(stateR2[:32], []byte("state_root_R2___________________")) // different root
	ism.State = stateR2
	err = suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism)
	suite.Require().NoError(err)

	// Reset the submissions flag (mirrors msg_server.go:97-101).
	err = suite.zkISMKeeper.SetMessageProofSubmitted(suite.ctx, ism.Id, false)
	suite.Require().NoError(err)

	// VULNERABILITY: messages[1] and messages[2] were authorized under R1 but
	// are still consumable after the ISM has transitioned to R2.
	for i, msg := range messages[1:] {
		has, err := suite.zkISMKeeper.HasMessageId(suite.ctx, ism.Id, msg.Id().Bytes())
		suite.Require().NoError(err)
		suite.Require().True(has, "message[%d] persists in store after state root change", i+1)

		authorized, err := suite.zkISMKeeper.Verify(suite.ctx, ism.Id, nil, msg)
		suite.Require().NoError(err)
		suite.Require().True(authorized, "VULNERABILITY: message[%d] authorized under R1 is still consumable under R2", i+1)
	}

	// Verify that consumed messages are removed (Verify does clean up after use).
	for _, msg := range messages[1:] {
		has, err := suite.zkISMKeeper.HasMessageId(suite.ctx, ism.Id, msg.Id().Bytes())
		suite.Require().NoError(err)
		suite.Require().False(has, "message should be removed after Verify consumed it")
	}
}
