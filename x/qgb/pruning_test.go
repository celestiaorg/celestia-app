package qgb_test

import (
	"testing"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/qgb"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// begin pruning attestations after 500 blocks
	qgb.SetTestPruningThreshold(500)
}

func TestPruning(t *testing.T) {
	// setup
	input, ctx := testutil.SetupFiveValChain(t)
	qgbKeeper := *input.QgbKeeper
	// set the data commitment window
	window := uint64(100)
	qgbKeeper.SetParams(ctx, types.Params{DataCommitmentWindow: window})

	// orderedTest is like a normal test, except they depend on the tests before
	// them to be executed in order.
	type orderedTest struct {
		step            string
		execUntilHeight int64
		newestAttNonce  uint64
		oldestAttNonce  uint64
		updateValSet    bool
	}

	// these tests assume that the PruningThreshold has been set to 500 blocks
	// and that they are executed in order
	tests := []orderedTest{
		{
			step:            "init chain, no pruning expected",
			execUntilHeight: 301,
			newestAttNonce:  4,
			oldestAttNonce:  1,
			updateValSet:    false,
		},
		// this test ensures that we don't prune every time the validator set
		// updates and acts as a control for an upcoming edge case
		{
			step:            "update the validator set, no pruning expected",
			execUntilHeight: 401,
			newestAttNonce:  6, // +2 one for the valset and one for a new data commitment
			oldestAttNonce:  1,
			updateValSet:    true,
		},
		{
			step:            "surpass pruning threshold, pruning expected",
			execUntilHeight: 801,
			newestAttNonce:  10,
			oldestAttNonce:  5, // note that we are pruning 4 attestations
			updateValSet:    false,
		},
		{
			step:            "surpass pruning threshold without updating the validator set, no pruning expected",
			execUntilHeight: 1001,
			newestAttNonce:  12,
			oldestAttNonce:  5, // note that we are not pruning despite the last attestation being > 500 blocks old
			updateValSet:    false,
		},
		{
			step:            "surpass pruning threshold but update the validator set, pruning expected",
			execUntilHeight: 1201,
			newestAttNonce:  15,
			oldestAttNonce:  10,
			updateValSet:    true,
		},
	}

	currentHeight := int64(1)

	for _, tt := range tests {
		t.Run(tt.step, func(t *testing.T) {
			if tt.updateValSet {
				updateValidatorSet(t, ctx, input)
			}
			// test that no prunning occurs if last unbonding height is still 0 (chain startup scenario)
			ctx = testutil.ExecuteQGBHeights(ctx, qgbKeeper, currentHeight, tt.execUntilHeight)

			currentHeight = tt.execUntilHeight

			oldestNonce := qgbKeeper.GetOldestAttestationNonce(ctx)
			newestNonce := qgbKeeper.GetLatestAttestationNonce(ctx)

			assert.Equal(t, tt.newestAttNonce, newestNonce)
			assert.Equal(t, tt.oldestAttNonce, oldestNonce)
		})
	}
}

func updateValidatorSet(t *testing.T, ctx sdktypes.Context, input testutil.TestInput) {
	msgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)

	newEVMAddr, err := teststaking.RandomEVMAddress()
	require.NoError(t, err)
	editMsg := stakingtypes.NewMsgEditValidator(
		testutil.ValAddrs[1],
		stakingtypes.Description{},
		nil,
		nil,
		newEVMAddr,
	)
	_, err = msgServer.EditValidator(ctx, editMsg)
	require.NoError(t, err)
	staking.EndBlocker(ctx, input.StakingKeeper)
}
