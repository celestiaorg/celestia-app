package qgb_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/x/qgb/types"

	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/qgb"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstAttestationIsValset(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	ctx = ctx.WithBlockHeight(1)
	// EndBlocker should set a new validator set
	qgb.EndBlocker(ctx, *pk)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
	attestation, found, err := pk.GetAttestationByNonce(ctx, 1)
	require.Nil(t, err)
	require.True(t, found)
	require.NotNil(t, attestation)
	require.Equal(t, uint64(1), attestation.GetNonce())

	// get the valset
	require.Equal(t, types.ValsetRequestType, attestation.Type())
	vs, ok := attestation.(*types.Valset)
	require.True(t, ok)
	require.NotNil(t, vs)
}

func TestValsetCreationWhenValidatorUnbonds(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	ctx = ctx.WithBlockHeight(1)
	// run abci methods after chain init
	staking.EndBlocker(ctx, input.StakingKeeper)
	qgb.EndBlocker(ctx, *pk)

	// current attestation expectedNonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := pk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	msgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)

	undelegateMsg := testutil.NewTestMsgUnDelegateValidator(testutil.ValAddrs[0], testutil.StakingAmount)
	_, err := msgServer.Undelegate(ctx, undelegateMsg)
	require.NoError(t, err)
	staking.EndBlocker(ctx, input.StakingKeeper)
	qgb.EndBlocker(ctx, *pk)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 10)

	assert.Equal(t, currentAttestationNonce+1, pk.GetLatestAttestationNonce(ctx))
}

func TestValsetCreationWhenEditingEVMAddr(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	ctx = ctx.WithBlockHeight(1)

	// run abci methods after chain init
	staking.EndBlocker(ctx, input.StakingKeeper)
	qgb.EndBlocker(ctx, *pk)

	// current attestation expectedNonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := pk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
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
	qgb.EndBlocker(ctx, *pk)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 10)

	assert.Equal(t, currentAttestationNonce+1, pk.GetLatestAttestationNonce(ctx))
}

func TestSetValset(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	vs, err := pk.GetCurrentValset(ctx)
	require.Nil(t, err)
	err = pk.SetAttestationRequest(ctx, &vs)
	require.Nil(t, err)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
}

func TestSetDataCommitment(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper

	ctx = ctx.WithBlockHeight(int64(qk.GetDataCommitmentWindowParam(ctx)))
	dc, err := qk.NextDataCommitment(ctx)
	require.NoError(t, err)
	err = qk.SetAttestationRequest(ctx, &dc)
	require.NoError(t, err)

	require.Equal(t, uint64(1), qk.GetLatestAttestationNonce(ctx))
}

// TestGetDataCommitment This test will test the creation of data commitment ranges
// in the event of the data commitment window changing via an upgrade or a gov proposal.
// The test goes as follows:
//   - Start with a data commitment window of 400
//   - Get the first data commitment, its range should be: [1, 400]
//   - Get the second data commitment, its range should be: [401, 800]
//   - Shrink the data commitment window to 101
//   - Get the third data commitment, its range should be: [801, 901]
//   - Expand the data commitment window to 500
//   - Get the fourth data commitment, its range should be: [902, 1401]
//
// Note: the table tests cannot be run separately. The reason we're using a table structure
// is to make it easy to understand the test flow.
func TestGetDataCommitment(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper

	tests := []struct {
		name       string
		window     uint64
		height     int64
		expectedDC types.DataCommitment
	}{
		{
			name:   "first data commitment for a window of 400",
			window: 400,
			height: 400,
			expectedDC: types.DataCommitment{
				Nonce:      1,
				BeginBlock: 1,
				EndBlock:   400,
			},
		},
		{
			name:   "second data commitment for a window of 400",
			window: 400,
			height: 800,
			expectedDC: types.DataCommitment{
				Nonce:      2,
				BeginBlock: 401,
				EndBlock:   800,
			},
		},
		{
			name:   "third data commitment after changing the window to 101",
			window: 101,
			height: 901,
			expectedDC: types.DataCommitment{
				Nonce:      3,
				BeginBlock: 801,
				EndBlock:   901,
			},
		},
		{
			name:   "fourth data commitment after changing the window to 500",
			window: 500,
			height: 1401,
			expectedDC: types.DataCommitment{
				Nonce:      4,
				BeginBlock: 902,
				EndBlock:   1401,
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

			// set the attestation request to be referenced by the next test cases
			err = qk.SetAttestationRequest(ctx, &dc)
			require.NoError(t, err)
		})
	}
}

func TestDataCommitmentCreation(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper

	ctx = ctx.WithBlockHeight(1)

	// run abci methods after chain init
	staking.EndBlocker(ctx, input.StakingKeeper)
	qgb.EndBlocker(ctx, *qk)

	// current attestation nonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := qk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	// increment height to be the same as the data commitment window
	newHeight := int64(qk.GetDataCommitmentWindowParam(ctx))
	ctx = ctx.WithBlockHeight(newHeight)
	qgb.EndBlocker(ctx, *qk)

	require.LessOrEqual(t, newHeight, ctx.BlockHeight())
	assert.Equal(t, uint64(2), qk.GetLatestAttestationNonce(ctx))
}

func TestDataCommitmentRange(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper

	ctx = ctx.WithBlockHeight(1)
	// run abci methods after chain init
	staking.EndBlocker(ctx, input.StakingKeeper)
	qgb.EndBlocker(ctx, *qk)

	// current attestation nonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := qk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	// increment height to be the same as the data commitment window
	newHeight := int64(qk.GetDataCommitmentWindowParam(ctx))
	ctx = ctx.WithBlockHeight(newHeight)
	qgb.EndBlocker(ctx, *qk)

	require.LessOrEqual(t, newHeight, ctx.BlockHeight())
	assert.Equal(t, uint64(2), qk.GetLatestAttestationNonce(ctx))

	att1, found, err := qk.GetAttestationByNonce(ctx, 2)
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, types.DataCommitmentRequestType, att1.Type())
	dc1, ok := att1.(*types.DataCommitment)
	require.True(t, ok)
	assert.Equal(t, newHeight, int64(dc1.EndBlock))
	assert.Equal(t, int64(1), int64(dc1.BeginBlock))

	// increment height to 2*data commitment window
	newHeight = int64(qk.GetDataCommitmentWindowParam(ctx)) * 2
	ctx = ctx.WithBlockHeight(newHeight)
	qgb.EndBlocker(ctx, *qk)

	att2, found, err := qk.GetAttestationByNonce(ctx, 3)
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, types.DataCommitmentRequestType, att2.Type())
	dc2, ok := att2.(*types.DataCommitment)
	require.True(t, ok)
	assert.Equal(t, newHeight, int64(dc2.EndBlock))
	assert.Equal(t, dc1.EndBlock+1, dc2.BeginBlock)
}

func TestHasDataCommitmentInStore(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper
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

// TestGetDataCommitment This test will test the data commitment creation catchup mechanism.
// It will run `abci.EndBlocker` on all the heights while changing the data commitment window
// in different occasions, to see if at the end of the test, the data commitments cover all
// the needed ranges.
func TestDataCommitmentCreationCatchup(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper
	ctx = ctx.WithBlockHeight(1)

	executeHeights := func(beginHeight int64, endHeight int64) {
		for i := beginHeight; i <= endHeight; i++ {
			ctx = ctx.WithBlockHeight(i)
			qgb.EndBlocker(ctx, *qk)
		}
	}

	// from height 1 to 1500 with a window of 400
	qk.SetParams(ctx, types.Params{DataCommitmentWindow: 400})
	executeHeights(1, 1500)

	// change window to 100 and execute up to 1920
	qk.SetParams(ctx, types.Params{DataCommitmentWindow: 100})
	executeHeights(1501, 1920)

	// change window to 1000 and execute up to 3500
	qk.SetParams(ctx, types.Params{DataCommitmentWindow: 1000})
	executeHeights(1921, 3500)

	// change window to 111 and execute up to 3800
	qk.SetParams(ctx, types.Params{DataCommitmentWindow: 111})
	executeHeights(3501, 3800)

	// check if a data commitment was created
	hasDataCommitment, err := qk.HasDataCommitmentInStore(ctx)
	require.NoError(t, err)
	require.True(t, hasDataCommitment)

	// get the last attestation nonce
	lastAttestationNonce := qk.GetLatestAttestationNonce(ctx)

	// check if the ranges are continuous
	var previousDC types.DataCommitment
	got := []types.DataCommitment{}
	for i := uint64(1); i <= lastAttestationNonce; i++ {
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
		assert.Equal(t, previousDC.EndBlock+1, dc.BeginBlock)
		previousDC = *dc
	}

	// we should have 19 data commitments created in the above setup
	// - window 400: [1, 400], [401, 800], [801, 1200]
	// - window 100: [1201, 1300], [1301, 1400], [1401, 1500], [1501, 1600], [1601, 1700], [1701, 1800], [1801, 1900]
	// - window 1000: [1901, 2900]
	// - window 111: [2901, 3011], [3012, 3122], [3123,3233], [3234, 3344], [3345, 3455], [3456, 3566], [3567, 3677], [3678, 3788]
	want := []types.DataCommitment{
		{
			Nonce:      2, // nonce 1 is the valset attestation
			BeginBlock: 1,
			EndBlock:   400,
		},
		{
			Nonce:      3,
			BeginBlock: 401,
			EndBlock:   800,
		},
		{
			Nonce:      4,
			BeginBlock: 801,
			EndBlock:   1200,
		},
		{
			Nonce:      5,
			BeginBlock: 1201,
			EndBlock:   1300,
		},
		{
			Nonce:      6,
			BeginBlock: 1301,
			EndBlock:   1400,
		},
		{
			Nonce:      7,
			BeginBlock: 1401,
			EndBlock:   1500,
		},
		{
			Nonce:      8,
			BeginBlock: 1501,
			EndBlock:   1600,
		},
		{
			Nonce:      9,
			BeginBlock: 1601,
			EndBlock:   1700,
		},
		{
			Nonce:      10,
			BeginBlock: 1701,
			EndBlock:   1800,
		},
		{
			Nonce:      11,
			BeginBlock: 1801,
			EndBlock:   1900,
		},
		{
			Nonce:      12,
			BeginBlock: 1901,
			EndBlock:   2900,
		},
		{
			Nonce:      13,
			BeginBlock: 2901,
			EndBlock:   3011,
		},
		{
			Nonce:      14,
			BeginBlock: 3012,
			EndBlock:   3122,
		},
		{
			Nonce:      15,
			BeginBlock: 3123,
			EndBlock:   3233,
		},
		{
			Nonce:      16,
			BeginBlock: 3234,
			EndBlock:   3344,
		},
		{
			Nonce:      17,
			BeginBlock: 3345,
			EndBlock:   3455,
		},
		{
			Nonce:      18,
			BeginBlock: 3456,
			EndBlock:   3566,
		},
		{
			Nonce:      19,
			BeginBlock: 3567,
			EndBlock:   3677,
		},
		{
			Nonce:      20,
			BeginBlock: 3678,
			EndBlock:   3788,
		},
	}
	assert.Equal(t, 19, len(got))
	assert.Equal(t, want, got)
}
