package qgb_test

import (
	"testing"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/qgb"
	qgbmodulekeeper "github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type PruningTestSuite struct {
	suite.Suite
	input     testutil.TestInput
	ctx       sdktypes.Context
	qgbKeeper qgbmodulekeeper.Keeper
}

func (s *PruningTestSuite) SetupSuite() {
	t := s.T()
	input, ctx := testutil.SetupFiveValChain(t)
	s.qgbKeeper = *input.QgbKeeper
	s.input = input
	s.ctx = ctx
}

func TestPruning(t *testing.T) {
	suite.Run(t, new(PruningTestSuite))
}

// TestAttestationsLowerThanPruningThreshold tests the first check for pruning, which is
// not pruning if the number of attestations is lower than the threshold.
func (s *PruningTestSuite) TestAttestationsLowerThanPruningThreshold() {
	// set the data commitment window
	window := uint64(101)
	s.qgbKeeper.SetParams(s.ctx, types.Params{DataCommitmentWindow: window})

	// test that no prunning occurs while we're still under the pruning threshold
	for attNonce := uint64(1); attNonce < qgb.AttestationPruningThreshold; attNonce++ {
		s.ctx = testutil.ExecuteQGBHeights(s.ctx, s.qgbKeeper, int64((attNonce-1)*window), int64(attNonce*window)+1)
		assert.NotPanics(s.T(), func() { qgb.PruneIfNeeded(s.ctx, s.qgbKeeper) })

		// the +1 serves to account for the valset created when initializing the chain
		totalNumberOfAttestations := attNonce + 1
		// check that the attestations are created
		assert.Equal(s.T(), totalNumberOfAttestations, s.qgbKeeper.GetLatestAttestationNonce(s.ctx))

		// check that all the attestations are still in state
		for nonce := uint64(1); nonce <= totalNumberOfAttestations; nonce++ {
			_, found, err := s.qgbKeeper.GetAttestationByNonce(s.ctx, nonce)
			assert.NoError(s.T(), err)
			assert.True(s.T(), found)
		}
	}
}

// TestAttestationsLowerThanPruningThreshold tests the chain startup case where there is no change
// to the validator set aside from the one in block 1.
func (s *PruningTestSuite) TestLastNonceHeightIsHigherThanLastUnbondingHeight() {
	// set the data commitment window
	window := uint64(101)
	s.qgbKeeper.SetParams(s.ctx, types.Params{DataCommitmentWindow: window})

	// test that no prunning occurs if last unbonding height is still 0 (chain startup scenario)
	s.ctx = testutil.ExecuteQGBHeights(s.ctx, s.qgbKeeper, 1, int64(qgb.AttestationPruningThreshold*window)+1)
	assert.NotPanics(s.T(), func() { qgb.PruneIfNeeded(s.ctx, s.qgbKeeper) })

	// the +1 serves to account for the valset created when initializing the chain
	totalNumberOfAttestations := uint64(qgb.AttestationPruningThreshold + 1)
	// check that the attestations are created
	assert.Equal(s.T(), totalNumberOfAttestations, s.qgbKeeper.GetLatestAttestationNonce(s.ctx))

	// check that all the attestations are still in state
	for nonce := uint64(1); nonce <= totalNumberOfAttestations; nonce++ {
		_, found, err := s.qgbKeeper.GetAttestationByNonce(s.ctx, nonce)
		assert.NoError(s.T(), err)
		assert.True(s.T(), found)
	}
}

// TestAttestationsLowerThanPruningThreshold tests that no pruning occurs even if we go over the
// AttestationPruningThreshold if the last available nonce height is that of the unbonding period.
func (s *PruningTestSuite) TestNoPruningUpToLastUnbondingNonce() {
	// set the data commitment window
	window := uint64(101)
	s.qgbKeeper.SetParams(s.ctx, types.Params{DataCommitmentWindow: window})

	// test that no prunning occurs if last unbonding height is still 0 (chain startup scenario)
	// but the attestations are way over the AttestationPruningThreshold
	s.ctx = testutil.ExecuteQGBHeights(s.ctx, s.qgbKeeper, 1, 2*int64(qgb.AttestationPruningThreshold*window)+1)
	assert.NotPanics(s.T(), func() { qgb.PruneIfNeeded(s.ctx, s.qgbKeeper) })

	// the +1 serves to account for the valset created when initializing the chain
	totalNumberOfAttestations := uint64(2*qgb.AttestationPruningThreshold + 1)
	// check that the attestations are created
	assert.Equal(s.T(), totalNumberOfAttestations, s.qgbKeeper.GetLatestAttestationNonce(s.ctx))

	// check that all the attestations are still in state
	for nonce := uint64(1); nonce <= totalNumberOfAttestations; nonce++ {
		_, found, err := s.qgbKeeper.GetAttestationByNonce(s.ctx, nonce)
		assert.NoError(s.T(), err)
		assert.True(s.T(), found)
	}
}

// TestPruneMultiple tests the case where the (LatestNonce-LastUnbondingNonce) > AttestationPruningThreshold
// But still will not prune because we want to keep attestations up to the last unbonding period.
// Then, sets the last unbonding height to a higher nonce to trigger multiple prunining rounds.
func (s *PruningTestSuite) TestPruneMultiple() {
	// set the data commitment window
	window := uint64(101)
	s.qgbKeeper.SetParams(s.ctx, types.Params{DataCommitmentWindow: window})

	// test that no prunning occurs if last unbonding height is still 0 (chain startup scenario)
	// but the attestations are way over the AttestationPruningThreshold
	s.ctx = testutil.ExecuteQGBHeights(s.ctx, s.qgbKeeper, 1, 2*int64(qgb.AttestationPruningThreshold*window)+1)
	assert.NotPanics(s.T(), func() { qgb.PruneIfNeeded(s.ctx, s.qgbKeeper) })

	// the +1 serves to account for the valset created when initializing the chain
	totalNumberOfAttestations := uint64(2*qgb.AttestationPruningThreshold + 1)
	// check that the attestations are created
	assert.Equal(s.T(), totalNumberOfAttestations, s.qgbKeeper.GetLatestAttestationNonce(s.ctx))

	// check that all the attestations are still in state
	for nonce := uint64(1); nonce <= totalNumberOfAttestations; nonce++ {
		_, found, err := s.qgbKeeper.GetAttestationByNonce(s.ctx, nonce)
		assert.NoError(s.T(), err)
		assert.True(s.T(), found)
	}

	// change the last unbonding height to a higher value
	updateValidatorSet(s.T(), s.ctx, s.input)
	s.qgbKeeper.SetLastUnBondingBlockHeight(s.ctx, uint64(s.ctx.BlockHeight()))
	s.qgbKeeper.SetLastUnbondingNonce(s.ctx, s.input.QgbKeeper.GetLatestAttestationNonce(s.ctx))
	qgb.EndBlocker(s.ctx, s.qgbKeeper)

	// number of attestations that should be pruned
	// the +2 is to account for the newly created valset that was set for the new unbonding height
	// and the fact that attestations start at nonce 1 and not 0.
	expectedLastPrunedAttestationNonce := totalNumberOfAttestations + 2 - qgb.AttestationPruningThreshold
	assert.Equal(s.T(), expectedLastPrunedAttestationNonce, s.qgbKeeper.CheckLastPrunedAttestationNonce(s.ctx))

	// check that the first attestations were pruned
	for nonce := uint64(1); nonce <= expectedLastPrunedAttestationNonce; nonce++ {
		_, found, err := s.qgbKeeper.GetAttestationByNonce(s.ctx, nonce)
		assert.NoError(s.T(), err)
		assert.False(s.T(), found)
	}

	// check that the attestations after those still exist
	for nonce := expectedLastPrunedAttestationNonce + 1; nonce <= s.qgbKeeper.GetLatestAttestationNonce(s.ctx); nonce++ {
		_, found, err := s.qgbKeeper.GetAttestationByNonce(s.ctx, nonce)
		assert.NoError(s.T(), err)
		assert.True(s.T(), found)
	}

	// continue running the chain for a few more blocks to be sure no inconsistency happens
	// after pruning
	testutil.ExecuteQGBHeights(s.ctx, s.qgbKeeper, 2*int64(qgb.AttestationPruningThreshold*window), 2*int64(qgb.AttestationPruningThreshold*window)+10)
}

// TestPruneOneByOne tests the case where the state machine prunes one by one, because the
// last unbonding nonce is way higher than the AttestationPruningThreshold and we can prune one
// by one attestations as new ones are created.
func (s *PruningTestSuite) TestPruneOneByOne() {
	// set the data commitment window
	window := uint64(101)
	s.qgbKeeper.SetParams(s.ctx, types.Params{DataCommitmentWindow: window})

	// test that no prunning occurs if last unbonding height is still 0 (chain startup scenario)
	// but the attestations are way over the AttestationPruningThreshold
	s.ctx = testutil.ExecuteQGBHeights(s.ctx, s.qgbKeeper, 1, int64(qgb.AttestationPruningThreshold*window+1))
	assert.NotPanics(s.T(), func() { qgb.PruneIfNeeded(s.ctx, s.qgbKeeper) })

	// the +1 serves to account for the valset created when initializing the chain
	totalNumberOfAttestations := uint64(qgb.AttestationPruningThreshold + 1)
	// check that the attestations are created
	assert.Equal(s.T(), totalNumberOfAttestations, s.qgbKeeper.GetLatestAttestationNonce(s.ctx))

	// check that all the attestations are still in state
	for nonce := uint64(1); nonce <= totalNumberOfAttestations; nonce++ {
		_, found, err := s.qgbKeeper.GetAttestationByNonce(s.ctx, nonce)
		assert.NoError(s.T(), err)
		assert.True(s.T(), found)
	}

	// change the last unbonding height to a higher value
	updateValidatorSet(s.T(), s.ctx, s.input)
	s.qgbKeeper.SetLastUnBondingBlockHeight(s.ctx, uint64(s.ctx.BlockHeight()))
	s.qgbKeeper.SetLastUnbondingNonce(s.ctx, s.input.QgbKeeper.GetLatestAttestationNonce(s.ctx))
	qgb.EndBlocker(s.ctx, s.qgbKeeper)

	lastPrunedNonce := s.qgbKeeper.GetLastPrunedAttestationNonce(s.ctx)
	lastUnbondingNonce := s.qgbKeeper.GetLastUnbondingNonce(s.ctx)
	// now, we should start seeing an attestation getting pruned as soon as a new one is created.
	// the +1 to account for the newly pruned nonce created when executing QGB heights inside the loop.
	// the -1 since we will keep the attestations from the last unbonding nonce (included)
	for nonce := lastPrunedNonce + 1; nonce < lastUnbondingNonce-1; nonce++ {
		currentHeight := s.ctx.BlockHeight()
		s.ctx = testutil.ExecuteQGBHeights(s.ctx, s.qgbKeeper, currentHeight, currentHeight+int64(window))
		_, found, err := s.qgbKeeper.GetAttestationByNonce(s.ctx, nonce)
		assert.NoError(s.T(), err)
		assert.False(s.T(), found)
		// check that we always have a constant number of attestations in store
		assert.Equal(s.T(), uint64(qgb.AttestationPruningThreshold), s.qgbKeeper.GetLatestAttestationNonce(s.ctx)-s.qgbKeeper.GetLastPrunedAttestationNonce(s.ctx))
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
