package qgb_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/x/qgb"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttestationCreationWhenStartingTheChain(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// EndBlocker should set a new validator set if not available
	qgb.EndBlocker(ctx, *pk)
	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
	attestation, found, err := pk.GetAttestationByNonce(ctx, 1)
	require.True(t, found)
	require.Nil(t, err)
	require.NotNil(t, attestation)
	require.Equal(t, uint64(1), attestation.GetNonce())
}

func TestValsetCreationUponUnbonding(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	currentValsetNonce := pk.GetLatestAttestationNonce(ctx)
	vs, err := pk.GetCurrentValset(ctx)
	require.Nil(t, err)
	err = pk.SetAttestationRequest(ctx, &vs)
	require.Nil(t, err)

	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	// begin unbonding
	msgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)
	undelegateMsg := testutil.NewTestMsgUnDelegateValidator(testutil.ValAddrs[0], testutil.StakingAmount)
	_, err = msgServer.Undelegate(input.Context, undelegateMsg)
	require.NoError(t, err)

	// Run the staking endblocker to ensure valset is set in state
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(input.Context, *pk)

	assert.NotEqual(t, currentValsetNonce, pk.GetLatestAttestationNonce(ctx))
}

func TestValsetEmission(t *testing.T) {
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

	// get the valsets
	require.Equal(t, types.ValsetRequestType, attestation.Type())
	vs, ok := attestation.(*types.Valset)
	require.True(t, ok)
	require.NotNil(t, vs)
}

func TestValsetSetting(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	vs, err := pk.GetCurrentValset(ctx)
	require.Nil(t, err)
	err = pk.SetAttestationRequest(ctx, &vs)
	require.Nil(t, err)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
}

// Add data commitment window tests

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
	assert.Equal(t, newHeight-1, int64(dc1.EndBlock))
	assert.Equal(t, int64(0), int64(dc1.BeginBlock))

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
	assert.Equal(t, newHeight-1, int64(dc2.EndBlock))
	assert.Equal(t, dc1.EndBlock+1, dc2.BeginBlock)
}
