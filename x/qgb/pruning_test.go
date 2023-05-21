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

// TestAttestationsLowerThanPruningThreshold tests the first check for pruning, which is
// not pruning if the number of attestations is lower than the threshold.
func TestAttestationsLowerThanPruningThreshold(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qgbKeeper := *input.QgbKeeper
	// set the data commitment window
	window := uint64(101)
	qgbKeeper.SetParams(ctx, types.Params{DataCommitmentWindow: window})

	// test that no prunning occurs while we're still under the pruning threshold
	for attNonce := uint64(1); attNonce < qgb.AttestationPruningThreshold; attNonce++ {
		ctx = testutil.ExecuteQGBHeights(ctx, qgbKeeper, int64((attNonce-1)*window), int64(attNonce*window)+1)
		assert.NotPanics(t, func() { qgb.PruneIfNeeded(ctx, qgbKeeper) })

		// the +1 serves to account for the valset created when initializing the chain
		totalNumberOfAttestations := attNonce + 1
		// check that the attestations are created
		assert.Equal(t, totalNumberOfAttestations, qgbKeeper.GetLatestAttestationNonce(ctx))

		// check that all the attestations are still in state
		for nonce := uint64(1); nonce <= totalNumberOfAttestations; nonce++ {
			_, found, err := qgbKeeper.GetAttestationByNonce(ctx, nonce)
			assert.NoError(t, err)
			assert.True(t, found)
		}
	}
}

// TestAttestationsLowerThanPruningThreshold tests the chain startup case where there is no change
// to the validator set aside from the one in block 1.
func TestLastNonceHeightIsHigherThanLastUnbondingHeight(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qgbKeeper := *input.QgbKeeper
	// set the data commitment window
	window := uint64(101)
	qgbKeeper.SetParams(ctx, types.Params{DataCommitmentWindow: window})

	// test that no prunning occurs if last unbonding height is still 0 (chain startup scenario)
	ctx = testutil.ExecuteQGBHeights(ctx, qgbKeeper, 1, int64(qgb.AttestationPruningThreshold*window)+1)
	assert.NotPanics(t, func() { qgb.PruneIfNeeded(ctx, qgbKeeper) })

	// the +1 serves to account for the valset created when initializing the chain
	totalNumberOfAttestations := uint64(qgb.AttestationPruningThreshold + 1)
	// check that the attestations are created
	assert.Equal(t, totalNumberOfAttestations, qgbKeeper.GetLatestAttestationNonce(ctx))

	// check that all the attestations are still in state
	for nonce := uint64(1); nonce <= totalNumberOfAttestations; nonce++ {
		_, found, err := qgbKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.True(t, found)
	}
}

// TestAttestationsLowerThanPruningThreshold tests that no pruning occurs even if we go over the
// AttestationPruningThreshold if the last available nonce height is that of the unbonding period.
func TestNoPruningUpToLastUnbondingNonce(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qgbKeeper := *input.QgbKeeper
	// set the data commitment window
	window := uint64(101)
	qgbKeeper.SetParams(ctx, types.Params{DataCommitmentWindow: window})

	// test that no prunning occurs if last unbonding height is still 0 (chain startup scenario)
	// but the attestations are way over the AttestationPruningThreshold
	ctx = testutil.ExecuteQGBHeights(ctx, qgbKeeper, 1, 2*int64(qgb.AttestationPruningThreshold*window)+1)
	assert.NotPanics(t, func() { qgb.PruneIfNeeded(ctx, qgbKeeper) })

	// the +1 serves to account for the valset created when initializing the chain
	totalNumberOfAttestations := uint64(2*qgb.AttestationPruningThreshold + 1)
	// check that the attestations are created
	assert.Equal(t, totalNumberOfAttestations, qgbKeeper.GetLatestAttestationNonce(ctx))

	// check that all the attestations are still in state
	for nonce := uint64(1); nonce <= totalNumberOfAttestations; nonce++ {
		_, found, err := qgbKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.True(t, found)
	}
}

// TestPruneMultiple tests the case where the (LatestNonce-LastUnbondingNonce) > AttestationPruningThreshold
// But still will not prune because we want to keep attestations up to the last unbonding period.
// Then, sets the last unbonding height to a higher nonce to trigger multiple pruning rounds.
func TestPruneMultiple(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qgbKeeper := *input.QgbKeeper
	// set the data commitment window
	window := uint64(101)
	qgbKeeper.SetParams(ctx, types.Params{DataCommitmentWindow: window})

	// test that no prunning occurs if last unbonding height is still 0 (chain startup scenario)
	// but the attestations are way over the AttestationPruningThreshold
	ctx = testutil.ExecuteQGBHeights(ctx, qgbKeeper, 1, 2*int64(qgb.AttestationPruningThreshold*window)+1)
	assert.NotPanics(t, func() { qgb.PruneIfNeeded(ctx, qgbKeeper) })

	// the +1 serves to account for the valset created when initializing the chain
	totalNumberOfAttestations := uint64(2*qgb.AttestationPruningThreshold + 1)
	// check that the attestations are created
	assert.Equal(t, totalNumberOfAttestations, qgbKeeper.GetLatestAttestationNonce(ctx))

	// check that all the attestations are still in state
	for nonce := uint64(1); nonce <= totalNumberOfAttestations; nonce++ {
		_, found, err := qgbKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.True(t, found)
	}

	// change the last unbonding height to a higher value
	updateValidatorSet(t, ctx, input)
	qgbKeeper.SetLastUnBondingBlockHeight(ctx, uint64(ctx.BlockHeight()))
	qgbKeeper.SetLastUnbondingNonce(ctx, qgbKeeper.GetLatestAttestationNonce(ctx))
	qgb.EndBlocker(ctx, qgbKeeper)

	// number of attestations that should be pruned
	// the +2 is to account for the newly created valset that was set for the new unbonding height
	// and the fact that attestations start at nonce 1 and not 0.
	expectedLastPrunedAttestationNonce := totalNumberOfAttestations + 2 - qgb.AttestationPruningThreshold
	assert.Equal(t, expectedLastPrunedAttestationNonce, qgbKeeper.GetLastPrunedAttestationNonce(ctx))

	// check that the first attestations were pruned
	for nonce := uint64(1); nonce <= expectedLastPrunedAttestationNonce; nonce++ {
		_, found, err := qgbKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.False(t, found)
	}

	// check that the attestations after those still exist
	for nonce := expectedLastPrunedAttestationNonce + 1; nonce <= qgbKeeper.GetLatestAttestationNonce(ctx); nonce++ {
		_, found, err := qgbKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.True(t, found)
	}

	// continue running the chain for a few more blocks to be sure no inconsistency happens
	// after pruning
	testutil.ExecuteQGBHeights(ctx, qgbKeeper, 2*int64(qgb.AttestationPruningThreshold*window), 2*int64(qgb.AttestationPruningThreshold*window)+10)
}

// TestPruneOneByOne tests the case where the state machine prunes one by one, because the
// last unbonding nonce is way higher than the AttestationPruningThreshold and we can prune one
// by one attestations as new ones are created.
func TestPruneOneByOne(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qgbKeeper := *input.QgbKeeper
	// set the data commitment window
	window := uint64(101)
	qgbKeeper.SetParams(ctx, types.Params{DataCommitmentWindow: window})

	// test that no prunning occurs if last unbonding height is still 0 (chain startup scenario)
	// but the attestations are way over the AttestationPruningThreshold
	ctx = testutil.ExecuteQGBHeights(ctx, qgbKeeper, 1, int64(qgb.AttestationPruningThreshold*window+1))
	assert.NotPanics(t, func() { qgb.PruneIfNeeded(ctx, qgbKeeper) })

	// the +1 serves to account for the valset created when initializing the chain
	totalNumberOfAttestations := uint64(qgb.AttestationPruningThreshold + 1)
	// check that the attestations are created
	assert.Equal(t, totalNumberOfAttestations, qgbKeeper.GetLatestAttestationNonce(ctx))

	// check that all the attestations are still in state
	for nonce := uint64(1); nonce <= totalNumberOfAttestations; nonce++ {
		_, found, err := qgbKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.True(t, found)
	}

	// change the last unbonding height to a higher value
	updateValidatorSet(t, ctx, input)
	qgbKeeper.SetLastUnBondingBlockHeight(ctx, uint64(ctx.BlockHeight()))
	qgbKeeper.SetLastUnbondingNonce(ctx, input.QgbKeeper.GetLatestAttestationNonce(ctx))
	qgb.EndBlocker(ctx, qgbKeeper)

	lastPrunedNonce := qgbKeeper.GetLastPrunedAttestationNonce(ctx)
	lastUnbondingNonce := qgbKeeper.GetLastUnbondingNonce(ctx)
	// now, we should start seeing an attestation getting pruned as soon as a new one is created.
	// the +1 to account for the newly pruned nonce created when executing QGB heights inside the loop.
	// the -1 since we will keep the attestations from the last unbonding nonce (included)
	for nonce := lastPrunedNonce + 1; nonce < lastUnbondingNonce-1; nonce++ {
		currentHeight := ctx.BlockHeight()
		ctx = testutil.ExecuteQGBHeights(ctx, qgbKeeper, currentHeight, currentHeight+int64(window))
		_, found, err := qgbKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.False(t, found)
		// check that we always have a constant number of attestations in store
		assert.Equal(t, uint64(qgb.AttestationPruningThreshold), qgbKeeper.GetLatestAttestationNonce(ctx)-qgbKeeper.GetLastPrunedAttestationNonce(ctx))
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
	qgb.EndBlocker(ctx, *input.QgbKeeper)
}
