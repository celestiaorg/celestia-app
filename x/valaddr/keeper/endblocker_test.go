package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/x/valaddr/keeper"
	"github.com/celestiaorg/celestia-app/v9/x/valaddr/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// mockStakingKeeper implements types.StakingKeeper. Only GetValidatorByConsAddr
// is exercised by the sweep; the others satisfy the interface.
type mockStakingKeeper struct {
	// byCons maps consAddr.String() to the validator returned for it. A missing
	// key models a validator that has been fully removed from staking state.
	byCons map[string]stakingtypes.Validator
}

func (m mockStakingKeeper) GetValidator(context.Context, sdk.ValAddress) (stakingtypes.Validator, error) {
	return stakingtypes.Validator{}, stakingtypes.ErrNoValidatorFound
}

func (m mockStakingKeeper) GetBondedValidatorsByPower(context.Context) ([]stakingtypes.Validator, error) {
	return nil, nil
}

func (m mockStakingKeeper) GetValidatorByConsAddr(_ context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error) {
	v, ok := m.byCons[consAddr.String()]
	if !ok {
		return stakingtypes.Validator{}, stakingtypes.ErrNoValidatorFound
	}
	return v, nil
}

func newTestKeeper(t *testing.T, staking types.StakingKeeper, blockTime time.Time) (keeper.Keeper, sdk.Context) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	tStoreKey := storetypes.NewTransientStoreKey("transient_test")
	testCtx := testutil.DefaultContextWithDB(t, storeKey, tStoreKey)
	ctx := testCtx.Ctx.WithBlockTime(blockTime)

	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	k := keeper.NewKeeper(cdc, runtime.NewKVStoreService(storeKey), log.NewNopLogger(), staking)
	return k, ctx
}

// TestRemoveFibreProviders covers each branch of the validator-lifecycle
// sweep: removed validators and long-jailed validators are deleted, while
// bonded and briefly-jailed validators keep their registration.
func TestRemoveFibreProviders(t *testing.T) {
	blockTime := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)

	bonded := sdk.ConsAddress("bonded_validator___")
	shortJail := sdk.ConsAddress("short_jail_validator")
	longJail := sdk.ConsAddress("long_jail_validator_")
	removed := sdk.ConsAddress("removed_validator__")

	staking := mockStakingKeeper{byCons: map[string]stakingtypes.Validator{
		// Active validator -> kept.
		bonded.String(): {Status: stakingtypes.Bonded, Jailed: false},
		// Jailed but only recently unbonded (within grace) -> kept.
		shortJail.String(): {
			Status:        stakingtypes.Unbonded,
			Jailed:        true,
			UnbondingTime: blockTime.Add(-24 * time.Hour),
		},
		// Jailed and unbonded well past the grace period -> deleted.
		longJail.String(): {
			Status:        stakingtypes.Unbonded,
			Jailed:        true,
			UnbondingTime: blockTime.Add(-types.JailedGracePeriod - 24*time.Hour),
		},
		// `removed` is intentionally absent -> GetValidatorByConsAddr returns
		// ErrNoValidatorFound -> deleted.
	}}

	k, ctx := newTestKeeper(t, staking, blockTime)

	info := types.FibreProviderInfo{Host: "host.example.com:7980"}
	for _, consAddr := range []sdk.ConsAddress{bonded, shortJail, longJail, removed} {
		require.NoError(t, k.SetFibreProviderInfo(ctx, consAddr, info))
	}

	require.NoError(t, k.RemoveFibreProviders(ctx))

	assertPresent := func(consAddr sdk.ConsAddress, want bool) {
		_, found := k.GetFibreProviderInfo(ctx, consAddr)
		require.Equal(t, want, found, "consAddr %s", consAddr)
	}

	assertPresent(bonded, true)    // still active
	assertPresent(shortJail, true) // jailed within grace
	assertPresent(longJail, false) // jailed past grace
	assertPresent(removed, false)  // removed from staking
}

// TestRemoveFibreProviders_GracePeriodBoundary verifies the entry survives
// right up to the grace threshold and is removed once it elapses.
func TestRemoveFibreProviders_GracePeriodBoundary(t *testing.T) {
	blockTime := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	consAddr := sdk.ConsAddress("boundary_validator_")

	jailedAt := func(unbondingTime time.Time) mockStakingKeeper {
		return mockStakingKeeper{byCons: map[string]stakingtypes.Validator{
			consAddr.String(): {Status: stakingtypes.Unbonded, Jailed: true, UnbondingTime: unbondingTime},
		}}
	}

	t.Run("exactly at threshold is kept", func(t *testing.T) {
		// blockTime == UnbondingTime + grace; deletion requires strictly after.
		k, ctx := newTestKeeper(t, jailedAt(blockTime.Add(-types.JailedGracePeriod)), blockTime)
		require.NoError(t, k.SetFibreProviderInfo(ctx, consAddr, types.FibreProviderInfo{Host: "h:1"}))
		require.NoError(t, k.RemoveFibreProviders(ctx))
		_, found := k.GetFibreProviderInfo(ctx, consAddr)
		require.True(t, found)
	})

	t.Run("one second past threshold is deleted", func(t *testing.T) {
		k, ctx := newTestKeeper(t, jailedAt(blockTime.Add(-types.JailedGracePeriod-time.Second)), blockTime)
		require.NoError(t, k.SetFibreProviderInfo(ctx, consAddr, types.FibreProviderInfo{Host: "h:1"}))
		require.NoError(t, k.RemoveFibreProviders(ctx))
		_, found := k.GetFibreProviderInfo(ctx, consAddr)
		require.False(t, found)
	})
}
