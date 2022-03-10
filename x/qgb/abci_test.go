package qgb

import (
	"github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestValsetCreationIfNotAvailable(t *testing.T) {
	input, ctx := keeper.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// EndBlocker should set a new validator set if not available
	EndBlocker(ctx, *pk)
	require.NotNil(t, pk.GetValset(ctx, pk.GetLatestValsetNonce(ctx)))
	valsets := pk.GetValsets(ctx)
	require.True(t, len(valsets) == 1)
}

func TestValsetCreationUponUnbonding(t *testing.T) {
	input, ctx := keeper.SetupFiveValChain(t)
	pk := input.QgbKeeper

	currentValsetNonce := pk.GetLatestValsetNonce(ctx)
	pk.SetValsetRequest(ctx)

	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	// begin unbonding
	sh := staking.NewHandler(input.StakingKeeper)
	undelegateMsg := keeper.NewTestMsgUnDelegateValidator(keeper.ValAddrs[0], keeper.StakingAmount)
	// nolint
	sh(input.Context, undelegateMsg)

	// Run the staking endblocker to ensure valset is set in state
	staking.EndBlocker(input.Context, input.StakingKeeper)
	EndBlocker(input.Context, *pk)

	assert.NotEqual(t, currentValsetNonce, pk.GetLatestValsetNonce(ctx))
}

func TestValsetEmission(t *testing.T) {
	input, ctx := keeper.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// Store a validator set with a power change as the most recent validator set
	vs, err := pk.GetCurrentValset(ctx)
	require.NoError(t, err)
	vs.Nonce--
	internalMembers, err := types.BridgeValidators(vs.Members).ToInternal()
	require.NoError(t, err)
	delta := float64(internalMembers.TotalPower()) * 0.05
	vs.Members[0].Power = uint64(float64(vs.Members[0].Power) - delta/2)
	vs.Members[1].Power = uint64(float64(vs.Members[1].Power) + delta/2)
	pk.StoreValset(ctx, vs)

	// EndBlocker should set a new validator set
	EndBlocker(ctx, *pk)
	require.NotNil(t, pk.GetValset(ctx, uint64(pk.GetLatestValsetNonce(ctx))))
	valsets := pk.GetValsets(ctx)
	require.True(t, len(valsets) == 2)
}

func TestValsetSetting(t *testing.T) {
	input, ctx := keeper.SetupFiveValChain(t)
	pk := input.QgbKeeper
	pk.SetValsetRequest(ctx)
	valsets := pk.GetValsets(ctx)
	require.True(t, len(valsets) == 1)
}

// Add data commitment window tests
