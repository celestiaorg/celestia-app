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

	// run abci methods after chain init
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(ctx, *pk)

	// current attestation expectedNonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := pk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	msgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)

	undelegateMsg := testutil.NewTestMsgUnDelegateValidator(testutil.ValAddrs[0], testutil.StakingAmount)
	_, err := msgServer.Undelegate(input.Context, undelegateMsg)
	require.NoError(t, err)
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(input.Context, *pk)
	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 10)

	assert.Equal(t, currentAttestationNonce+1, pk.GetLatestAttestationNonce(ctx))
}

func TestValsetCreationWhenEditingEVMAddr(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// run abci methods after chain init
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(ctx, *pk)

	// current attestation expectedNonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := pk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
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
	_, err = msgServer.EditValidator(input.Context, editMsg)
	require.NoError(t, err)
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(input.Context, *pk)
	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 10)

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
	dc, err := qk.GetCurrentDataCommitment(ctx)
	require.NoError(t, err)
	err = qk.SetAttestationRequest(ctx, &dc)
	require.NoError(t, err)

	require.Equal(t, uint64(1), qk.GetLatestAttestationNonce(input.Context))
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
		name   string
		window uint64
		// to simulate the height condition in endBlocker: ctx.BlockHeight()%int64(k.GetDataCommitmentWindowParam(ctx))
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
			dc, err := qk.GetCurrentDataCommitment(ctx)
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

	// run abci methods after chain init
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(ctx, *qk)

	// current attestation nonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := qk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	// increment height to be the same as the data commitment window
	newHeight := int64(qk.GetDataCommitmentWindowParam(ctx))
	input.Context = ctx.WithBlockHeight(newHeight)
	qgb.EndBlocker(input.Context, *qk)

	require.Less(t, newHeight, ctx.BlockHeight())
	assert.Equal(t, uint64(2), qk.GetLatestAttestationNonce(ctx))
}

func TestDataCommitmentRange(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper

	// run abci methods after chain init
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(ctx, *qk)

	// current attestation nonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := qk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	// increment height to be the same as the data commitment window
	newHeight := int64(qk.GetDataCommitmentWindowParam(ctx))
	input.Context = ctx.WithBlockHeight(newHeight)
	qgb.EndBlocker(input.Context, *qk)

	require.Less(t, newHeight, ctx.BlockHeight())
	assert.Equal(t, uint64(2), qk.GetLatestAttestationNonce(ctx))

	att1, found, err := qk.GetAttestationByNonce(input.Context, 2)
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, types.DataCommitmentRequestType, att1.Type())
	dc1, ok := att1.(*types.DataCommitment)
	require.True(t, ok)
	assert.Equal(t, newHeight, int64(dc1.EndBlock))
	assert.Equal(t, int64(1), int64(dc1.BeginBlock))

	// increment height to 2*data commitment window
	newHeight = int64(qk.GetDataCommitmentWindowParam(ctx)) * 2
	input.Context = ctx.WithBlockHeight(newHeight)
	qgb.EndBlocker(input.Context, *qk)

	att2, found, err := qk.GetAttestationByNonce(input.Context, 3)
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, types.DataCommitmentRequestType, att2.Type())
	dc2, ok := att2.(*types.DataCommitment)
	require.True(t, ok)
	assert.Equal(t, newHeight, int64(dc2.EndBlock))
	assert.Equal(t, dc1.EndBlock+1, dc2.BeginBlock)
}
