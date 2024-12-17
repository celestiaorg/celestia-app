package blobstream_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/x/blobstream"

	"github.com/celestiaorg/celestia-app/v3/x/blobstream/keeper"
	"github.com/celestiaorg/celestia-app/v3/x/blobstream/types"

	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstAttestationIsValset(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.BlobstreamKeeper

	ctx = ctx.WithBlockHeight(1)
	expectedTime := ctx.BlockTime()
	// EndBlocker should set a new validator set
	blobstream.EndBlocker(ctx, pk)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
	attestation, found, err := pk.GetAttestationByNonce(ctx, 1)
	require.Nil(t, err)
	require.True(t, found)
	require.NotNil(t, attestation)
	require.Equal(t, uint64(1), attestation.GetNonce())

	// get the valset
	vs, ok := attestation.(*types.Valset)
	assert.True(t, ok)
	assert.NotNil(t, vs)
	assert.Equal(t, expectedTime, vs.Time)
}

func TestValsetCreationWhenValidatorUnbonds(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.BlobstreamKeeper

	ctx = ctx.WithBlockHeight(1)
	// run abci methods after chain init
	staking.EndBlocker(ctx, input.StakingKeeper)
	blobstream.EndBlocker(ctx, pk)

	// current attestation expectedNonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := pk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	msgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)

	undelegateMsg := testutil.NewTestMsgUnDelegateValidator(testutil.ValAddrs[0], testutil.StakingAmount)
	_, err := msgServer.Undelegate(ctx, undelegateMsg)
	require.NoError(t, err)
	staking.EndBlocker(ctx, input.StakingKeeper)
	blobstream.EndBlocker(ctx, pk)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 10)

	assert.Equal(t, currentAttestationNonce+1, pk.GetLatestAttestationNonce(ctx))
}

func TestValsetCreationWhenEditingEVMAddr(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.BlobstreamKeeper

	ctx = ctx.WithBlockHeight(1)

	// run abci methods after chain init
	staking.EndBlocker(ctx, input.StakingKeeper)
	blobstream.EndBlocker(ctx, pk)

	// current attestation expectedNonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := pk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	msgServer := keeper.NewMsgServerImpl(input.BlobstreamKeeper)

	newEVMAddr := testfactory.RandomEVMAddress()
	registerMsg := types.NewMsgRegisterEVMAddress(
		testutil.ValAddrs[1],
		newEVMAddr,
	)
	_, err := msgServer.RegisterEVMAddress(ctx, registerMsg)
	require.NoError(t, err)
	staking.EndBlocker(ctx, input.StakingKeeper)
	blobstream.EndBlocker(ctx, pk)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 10)

	assert.Equal(t, currentAttestationNonce+1, pk.GetLatestAttestationNonce(ctx))
}

func TestSetValset(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.BlobstreamKeeper

	vs, err := pk.GetCurrentValset(ctx)
	require.Nil(t, err)
	err = pk.SetAttestationRequest(ctx, &vs)
	require.Nil(t, err)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
}

func TestSetDataCommitment(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.BlobstreamKeeper

	ctx = ctx.WithBlockHeight(int64(qk.GetDataCommitmentWindowParam(ctx)))
	expectedTime := ctx.BlockTime()
	dc, err := qk.NextDataCommitment(ctx)
	require.NoError(t, err)
	err = qk.SetAttestationRequest(ctx, &dc)
	require.NoError(t, err)

	require.Equal(t, uint64(1), qk.GetLatestAttestationNonce(ctx))
	attestation, found, err := qk.GetAttestationByNonce(ctx, 1)
	require.Nil(t, err)
	require.True(t, found)
	require.NotNil(t, attestation)
	require.Equal(t, uint64(1), attestation.GetNonce())

	// get the data commitment
	actualDC, ok := attestation.(*types.DataCommitment)
	assert.True(t, ok)
	assert.NotNil(t, actualDC)
	assert.Equal(t, expectedTime, actualDC.Time)
}

// TestGetDataCommitment This test will test the creation of data commitment
// ranges in the event of the data commitment window changing via an upgrade or
// a gov proposal. The test goes as follows:
//   - Start with a data commitment window of 400
//   - Get the first data commitment, its range should be: [1, 401)
//   - Get the second data commitment, its range should be: [401, 801)
//   - Shrink the data commitment window to 101
//   - Get the third data commitment, its range should be: [801, 902)
//   - Expand the data commitment window to 500
//   - Get the fourth data commitment, its range should be: [902, 1402)
//
// Note: the table tests cannot be run separately. The reason we're using a
// table structure is to make it easy to understand the test flow.
func TestGetDataCommitment(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.BlobstreamKeeper

	tests := []struct {
		name       string
		window     uint64
		height     int64
		expectedDC types.DataCommitment
	}{
		{
			name:   "first data commitment for a window of 400",
			window: 400,
			height: 401,
			expectedDC: types.DataCommitment{
				Nonce:      1,
				BeginBlock: 1,
				EndBlock:   401,
			},
		},
		{
			name:   "second data commitment for a window of 400",
			window: 400,
			height: 801,
			expectedDC: types.DataCommitment{
				Nonce:      2,
				BeginBlock: 401,
				EndBlock:   801,
			},
		},
		{
			name:   "third data commitment after changing the window to 101",
			window: 101,
			height: 902,
			expectedDC: types.DataCommitment{
				Nonce:      3,
				BeginBlock: 801,
				EndBlock:   902,
			},
		},
		{
			name:   "fourth data commitment after changing the window to 500",
			window: 500,
			height: 1402,
			expectedDC: types.DataCommitment{
				Nonce:      4,
				BeginBlock: 902,
				EndBlock:   1402,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// set the data commitment window
			qk.SetParams(ctx, types.Params{DataCommitmentWindow: tt.window})
			require.Equal(t, tt.window, qk.GetDataCommitmentWindowParam(ctx))

			// change the block height
			ctx = ctx.WithBlockHeight(tt.height)

			// get the data commitment
			dc, err := qk.NextDataCommitment(ctx)
			require.NoError(t, err)
			require.Equal(t, tt.expectedDC.BeginBlock, dc.BeginBlock)
			require.Equal(t, tt.expectedDC.EndBlock, dc.EndBlock)
			require.Equal(t, tt.expectedDC.Nonce, dc.Nonce)

			// set the attestation request to be referenced by the next test
			// cases
			err = qk.SetAttestationRequest(ctx, &dc)
			require.NoError(t, err)
		})
	}
}

func TestDataCommitmentCreation(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.BlobstreamKeeper

	ctx = ctx.WithBlockHeight(1)

	// run abci methods after chain init
	staking.EndBlocker(ctx, input.StakingKeeper)
	blobstream.EndBlocker(ctx, qk)

	// current attestation nonce should be 1 because a valset has been emitted
	// upon chain init.
	currentAttestationNonce := qk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	// increment height to be the same as the data commitment window
	newHeight := int64(qk.GetDataCommitmentWindowParam(ctx))
	ctx = ctx.WithBlockHeight(newHeight)
	blobstream.EndBlocker(ctx, qk)

	require.LessOrEqual(t, newHeight, ctx.BlockHeight())
	assert.Equal(t, uint64(2), qk.GetLatestAttestationNonce(ctx))
}

func TestDataCommitmentRange(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.BlobstreamKeeper

	ctx = ctx.WithBlockHeight(1)
	// run abci methods after chain init
	staking.EndBlocker(ctx, input.StakingKeeper)
	blobstream.EndBlocker(ctx, qk)

	// current attestation nonce should be 1 because a valset has been emitted
	// upon chain init.
	currentAttestationNonce := qk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	// increment height to be the same as the data commitment window
	newHeight := int64(qk.GetDataCommitmentWindowParam(ctx)) + 1
	ctx = ctx.WithBlockHeight(newHeight)
	blobstream.EndBlocker(ctx, qk)

	require.LessOrEqual(t, newHeight, ctx.BlockHeight())
	assert.Equal(t, uint64(2), qk.GetLatestAttestationNonce(ctx))

	att1, found, err := qk.GetAttestationByNonce(ctx, 2)
	require.NoError(t, err)
	require.True(t, found)

	dc1, ok := att1.(*types.DataCommitment)
	require.True(t, ok)
	assert.Equal(t, newHeight, int64(dc1.EndBlock))
	assert.Equal(t, int64(1), int64(dc1.BeginBlock))

	// increment height to 2*data commitment window
	newHeight = int64(qk.GetDataCommitmentWindowParam(ctx))*2 + 1
	ctx = ctx.WithBlockHeight(newHeight)
	blobstream.EndBlocker(ctx, qk)

	att2, found, err := qk.GetAttestationByNonce(ctx, 3)
	require.NoError(t, err)
	require.True(t, found)

	dc2, ok := att2.(*types.DataCommitment)
	require.True(t, ok)
	assert.Equal(t, newHeight, int64(dc2.EndBlock))
	assert.Equal(t, dc1.EndBlock, dc2.BeginBlock)
}

func TestHasDataCommitmentInStore(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.BlobstreamKeeper
	// set the data commitment window
	qk.SetParams(ctx, types.Params{DataCommitmentWindow: 400})
	require.Equal(t, uint64(400), qk.GetDataCommitmentWindowParam(ctx))

	tests := []struct {
		name         string
		setup        func()
		expectExists bool
	}{
		{
			name:         "when store has no attestation set",
			setup:        func() {},
			expectExists: false,
		},
		{
			name: "when store has one valset attestation set",
			setup: func() {
				vs, err := qk.GetCurrentValset(ctx)
				require.NoError(t, err)
				err = qk.SetAttestationRequest(ctx, &vs)
				require.NoError(t, err)
			},
			expectExists: false,
		},
		{
			name: "when store has 2 valsets set",
			setup: func() {
				vs, err := qk.GetCurrentValset(ctx)
				require.NoError(t, err)
				err = qk.SetAttestationRequest(ctx, &vs)
				require.NoError(t, err)
			},
			expectExists: false,
		},
		{
			name: "when store has one data commitment",
			setup: func() {
				ctx = ctx.WithBlockHeight(400)
				dc, err := qk.NextDataCommitment(ctx)
				require.NoError(t, err)
				err = qk.SetAttestationRequest(ctx, &dc)
				require.NoError(t, err)
			},
			expectExists: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			exists, err := qk.HasDataCommitmentInStore(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.expectExists, exists)
		})
	}
}

// TestDataCommitmentCreationCatchup This test will test the data commitment creation
// catchup mechanism. It will run `abci.EndBlocker` on all the heights while
// changing the data commitment window in different occasions, to see if at the
// end of the test, the data commitments cover all the needed ranges.
func TestDataCommitmentCreationCatchup(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.BlobstreamKeeper
	ctx = ctx.WithBlockHeight(1)

	// from height 1 to 1500 with a window of 400
	qk.SetParams(ctx, types.Params{DataCommitmentWindow: 400})
	ctx = testutil.ExecuteBlobstreamHeights(ctx, qk, 1, 1501)

	// change window to 100 and execute up to 1920
	qk.SetParams(ctx, types.Params{DataCommitmentWindow: 100})
	ctx = testutil.ExecuteBlobstreamHeights(ctx, qk, 1501, 1921)

	// change window to 1000 and execute up to 3500
	qk.SetParams(ctx, types.Params{DataCommitmentWindow: 1000})
	ctx = testutil.ExecuteBlobstreamHeights(ctx, qk, 1921, 3501)

	// change window to 111 and execute up to 3800
	qk.SetParams(ctx, types.Params{DataCommitmentWindow: 111})
	ctx = testutil.ExecuteBlobstreamHeights(ctx, qk, 3501, 3801)

	// check if a data commitment was created
	hasDataCommitment, err := qk.HasDataCommitmentInStore(ctx)
	require.NoError(t, err)
	require.True(t, hasDataCommitment)

	// get the latest attestation nonce
	latestAttestationNonce := qk.GetLatestAttestationNonce(ctx)

	// check if the ranges are continuous
	var previousDC types.DataCommitment
	got := []types.DataCommitment{}
	for i := uint64(1); i <= latestAttestationNonce; i++ {
		att, found, err := qk.GetAttestationByNonce(ctx, i)
		require.NoError(t, err)
		require.True(t, found)
		dc, ok := att.(*types.DataCommitment)
		if !ok {
			continue
		}
		got = append(got, *dc)
		if previousDC.Nonce == 0 {
			// initialize the previous dc
			previousDC = *dc
			continue
		}
		assert.Equal(t, previousDC.EndBlock, dc.BeginBlock)
		previousDC = *dc
	}

	// we should have 19 data commitments created in the above setup
	// - window 400: [1, 401), [401, 801), [801, 1201)
	// - window 100: [1201, 1301), [1301, 1401), [1401, 1501), [1501, 1601), [1601, 1701), [1701, 1801), [1801, 1901)
	// - window 1000: [1901, 2901[
	// - window 111: [2901, 3012), [3012, 3123), [3123,3234), [3234, 3345), [3345, 3456), [3456, 3567), [3567, 3678), [3678, 3789)
	want := []types.DataCommitment{
		{
			Nonce:      2, // nonce 1 is the valset attestation
			BeginBlock: 1,
			EndBlock:   401,
		},
		{
			Nonce:      3,
			BeginBlock: 401,
			EndBlock:   801,
		},
		{
			Nonce:      4,
			BeginBlock: 801,
			EndBlock:   1201,
		},
		{
			Nonce:      5,
			BeginBlock: 1201,
			EndBlock:   1301,
		},
		{
			Nonce:      6,
			BeginBlock: 1301,
			EndBlock:   1401,
		},
		{
			Nonce:      7,
			BeginBlock: 1401,
			EndBlock:   1501,
		},
		{
			Nonce:      8,
			BeginBlock: 1501,
			EndBlock:   1601,
		},
		{
			Nonce:      9,
			BeginBlock: 1601,
			EndBlock:   1701,
		},
		{
			Nonce:      10,
			BeginBlock: 1701,
			EndBlock:   1801,
		},
		{
			Nonce:      11,
			BeginBlock: 1801,
			EndBlock:   1901,
		},
		{
			Nonce:      12,
			BeginBlock: 1901,
			EndBlock:   2901,
		},
		{
			Nonce:      13,
			BeginBlock: 2901,
			EndBlock:   3012,
		},
		{
			Nonce:      14,
			BeginBlock: 3012,
			EndBlock:   3123,
		},
		{
			Nonce:      15,
			BeginBlock: 3123,
			EndBlock:   3234,
		},
		{
			Nonce:      16,
			BeginBlock: 3234,
			EndBlock:   3345,
		},
		{
			Nonce:      17,
			BeginBlock: 3345,
			EndBlock:   3456,
		},
		{
			Nonce:      18,
			BeginBlock: 3456,
			EndBlock:   3567,
		},
		{
			Nonce:      19,
			BeginBlock: 3567,
			EndBlock:   3678,
		},
		{
			Nonce:      20,
			BeginBlock: 3678,
			EndBlock:   3789,
		},
	}
	assert.Equal(t, 19, len(got))
	for i, dc := range got {
		// we don't care about checking the time of the blocks at this level
		assert.Equal(t, want[i].EndBlock, dc.EndBlock)
		assert.Equal(t, want[i].BeginBlock, dc.BeginBlock)
		assert.Equal(t, want[i].Nonce, dc.Nonce)
	}
}

// TestPruning tests the pruning mechanism by: 1. Generating a set of
// attestations 2. Running the Blobstream EndBlocker 3. Verifying that the expired
// attestations are pruned
func TestPruning(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	bsKeeper := input.BlobstreamKeeper
	// set the data commitment window
	window := uint64(101)
	bsKeeper.SetParams(ctx, types.Params{DataCommitmentWindow: window})
	initialBlockTime := ctx.BlockTime()
	blockInterval := 10 * time.Minute
	ctx = testutil.ExecuteBlobstreamHeightsWithTime(ctx, bsKeeper, 1, 1626, blockInterval)

	// check that we created a number of attestations
	assert.Equal(t, uint64(17), bsKeeper.GetLatestAttestationNonce(ctx))

	// check that no pruning occurs if no attestations expired
	for nonce := uint64(1); nonce <= bsKeeper.GetLatestAttestationNonce(ctx); nonce++ {
		_, found, err := bsKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.True(t, found)
	}

	// continue executing heights
	ctx = testutil.ExecuteBlobstreamHeightsWithTime(ctx, bsKeeper, 1626, 5000, blockInterval)

	earliestAttestationNonce := bsKeeper.GetEarliestAvailableAttestationNonce(ctx)
	assert.Equal(t, uint64(21), earliestAttestationNonce)

	// check that the first attestations were pruned
	for nonce := uint64(1); nonce < earliestAttestationNonce; nonce++ {
		_, found, err := bsKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.False(t, found)
	}

	// check that the attestations after those still exist
	for nonce := bsKeeper.GetEarliestAvailableAttestationNonce(ctx); nonce <= bsKeeper.GetLatestAttestationNonce(ctx); nonce++ {
		at, found, err := bsKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.True(t, found)
		// make sure the remaining attestations have not expired yet
		assert.True(t, initialBlockTime.Before(at.BlockTime().Add(blobstream.AttestationExpiryTime)))
	}

	// check that no valset exists in store
	for nonce := bsKeeper.GetEarliestAvailableAttestationNonce(ctx); nonce <= bsKeeper.GetLatestAttestationNonce(ctx); nonce++ {
		at, found, err := bsKeeper.GetAttestationByNonce(ctx, nonce)
		assert.NoError(t, err)
		assert.True(t, found)
		_, ok := at.(*types.DataCommitment)
		assert.True(t, ok)
	}

	// check that we still can get a valset even after pruning all of them
	vs, err := bsKeeper.GetLatestValset(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, vs)

	// continue running the chain for a few more blocks to be sure no
	// inconsistency happens after pruning
	testutil.ExecuteBlobstreamHeightsWithTime(ctx, bsKeeper, 5000, 6000, blockInterval)
}
