package qgb_test

import (
	"github.com/celestiaorg/celestia-app/x/qgb"
	"github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAttestationCreationWhenStartingTheChain(t *testing.T) {
	input, ctx := keeper.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// EndBlocker should set a new validator set if not available
	qgb.EndBlocker(ctx, *pk)
	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
	attestation := pk.GetAttestationByNonce(ctx, 1)
	require.NotNil(t, attestation)
	require.Equal(t, uint64(1), attestation.GetNonce())
}

func TestValsetCreationUponUnbonding(t *testing.T) {
	input, ctx := keeper.SetupFiveValChain(t)
	pk := input.QgbKeeper

	currentValsetNonce := pk.GetLatestAttestationNonce(ctx)
	vs, err := pk.GetCurrentValset(ctx)
	require.Nil(t, err)
	err = pk.SetAttestationRequest(ctx, &vs)
	require.Nil(t, err)

	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	// begin unbonding
	sh := staking.NewHandler(input.StakingKeeper)
	undelegateMsg := keeper.NewTestMsgUnDelegateValidator(keeper.ValAddrs[0], keeper.StakingAmount)
	// nolint
	sh(input.Context, undelegateMsg)

	// Run the staking endblocker to ensure valset is set in state
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(input.Context, *pk)

	assert.NotEqual(t, currentValsetNonce, pk.GetLatestAttestationNonce(ctx))
}

func TestValsetEmission(t *testing.T) {
	input, ctx := keeper.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// EndBlocker should set a new validator set
	qgb.EndBlocker(ctx, *pk)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
	attestation := pk.GetAttestationByNonce(ctx, 1)
	require.NotNil(t, attestation)
	require.Equal(t, uint64(1), attestation.GetNonce())

	// get the valsets
	require.Equal(t, types.ValsetRequestType, attestation.Type())
	vs, ok := attestation.(*types.Valset)
	require.True(t, ok)
	require.NotNil(t, vs)
}

func TestValsetSetting(t *testing.T) {
	input, ctx := keeper.SetupFiveValChain(t)
	pk := input.QgbKeeper

	vs, err := pk.GetCurrentValset(ctx)
	require.Nil(t, err)
	err = pk.SetAttestationRequest(ctx, &vs)
	require.Nil(t, err)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
}

// Add data commitment window tests
