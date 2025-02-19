package signal_test

import (
	"context"
	"cosmossdk.io/core/header"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"fmt"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v2"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/x/signal"
	"github.com/celestiaorg/celestia-app/v4/x/signal/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtversion "github.com/cometbft/cometbft/proto/tendermint/version"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math"
	"math/big"
	"testing"
)

func TestGetVotingPowerThreshold(t *testing.T) {
	bigInt := big.NewInt(0)
	bigInt.SetString("23058430092136939509", 10)

	type testCase struct {
		name       string
		validators map[string]int64
		want       sdkmath.Int
	}
	testCases := []testCase{
		{
			name:       "empty validators",
			validators: map[string]int64{},
			want:       sdkmath.NewInt(0),
		},
		{
			name:       "one validator with 6 power returns 5 because the defaultSignalThreshold is 5/6",
			validators: map[string]int64{"a": 6},
			want:       sdkmath.NewInt(5),
		},
		{
			name:       "one validator with 11 power (11 * 5/6 = 9.16666667) so should round up to 10",
			validators: map[string]int64{"a": 11},
			want:       sdkmath.NewInt(10),
		},
		{
			name:       "one validator with voting power of math.MaxInt64",
			validators: map[string]int64{"a": math.MaxInt64},
			want:       sdkmath.NewInt(7686143364045646503),
		},
		{
			name:       "multiple validators with voting power of math.MaxInt64",
			validators: map[string]int64{"a": math.MaxInt64, "b": math.MaxInt64, "c": math.MaxInt64},
			want:       sdkmath.NewIntFromBigInt(bigInt),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
			stakingKeeper := newMockStakingKeeper(tc.validators)
			k := signal.NewKeeper(config.Codec, nil, stakingKeeper)
			got, err := k.GetVotingPowerThreshold(sdk.Context{})
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got, fmt.Sprintf("want %v, got %v", tc.want.String(), got.String()))
		})
	}
}

func TestSignalVersion(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)
	t.Run("should return an error if the signal version is less than the current version", func(t *testing.T) {
		_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
			ValidatorAddress: testutil.ValAddrs[0].String(),
			Version:          0,
		})
		assert.Error(t, err)
		assert.ErrorIs(t, err, types.ErrInvalidSignalVersion)
	})
	t.Run("should not return an error if the signal version is greater than the next version", func(t *testing.T) {
		_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
			ValidatorAddress: testutil.ValAddrs[0].String(),
			Version:          3,
		})
		assert.NoError(t, err)
	})
	t.Run("should return an error if the validator was not found", func(t *testing.T) {
		_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
			ValidatorAddress: testutil.ValAddrs[4].String(),
			Version:          2,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, stakingtypes.ErrNoValidatorFound)
	})
	t.Run("should not return an error if the signal version and validator are valid", func(t *testing.T) {
		_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
			ValidatorAddress: testutil.ValAddrs[0].String(),
			Version:          2,
		})
		require.NoError(t, err)

		res, err := upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
			Version: 2,
		})
		require.NoError(t, err)
		require.EqualValues(t, 40, res.VotingPower)
		require.EqualValues(t, 100, res.ThresholdPower)
		require.EqualValues(t, 120, res.TotalVotingPower)
	})
}

func TestTallyingLogic(t *testing.T) {
	upgradeKeeper, ctx, mockStakingKeeper := setup(t)
	_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[0].String(),
		Version:          0,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidSignalVersion)

	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[0].String(),
		Version:          3,
	})
	require.NoError(t, err) // version 3 is valid because it is greater than the current version

	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[0].String(),
		Version:          2,
	})
	require.NoError(t, err)

	res, err := upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 40, res.VotingPower)
	require.EqualValues(t, 100, res.ThresholdPower)
	require.EqualValues(t, 120, res.TotalVotingPower)

	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[2].String(),
		Version:          2,
	})
	require.NoError(t, err)

	res, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 99, res.VotingPower)
	require.EqualValues(t, 100, res.ThresholdPower)
	require.EqualValues(t, 120, res.TotalVotingPower)

	_, err = upgradeKeeper.TryUpgrade(ctx, &types.MsgTryUpgrade{})
	require.NoError(t, err)
	shouldUpgrade, version := upgradeKeeper.ShouldUpgrade(ctx)
	require.False(t, shouldUpgrade)
	require.Equal(t, uint64(0), version)

	// we now have 101/120
	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[1].String(),
		Version:          2,
	})
	require.NoError(t, err)

	_, err = upgradeKeeper.TryUpgrade(ctx, &types.MsgTryUpgrade{})
	require.NoError(t, err)

	shouldUpgrade, version = upgradeKeeper.ShouldUpgrade(ctx)
	require.False(t, shouldUpgrade) // should be false because upgrade height hasn't been reached.
	require.Equal(t, uint64(0), version)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + appconsts.UpgradeHeightDelay(appconsts.TestChainID, version))

	shouldUpgrade, version = upgradeKeeper.ShouldUpgrade(ctx)
	require.True(t, shouldUpgrade) // should be true because upgrade height has been reached.
	require.Equal(t, v2.Version, version)

	upgradeKeeper.ResetTally(ctx)

	// update the version to 2
	ctx = ctx.WithBlockHeader(tmproto.Header{
		Version: cmtversion.Consensus{
			Block: 1,
			App:   2,
		},
	})

	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[0].String(),
		Version:          3,
	})
	require.NoError(t, err)
	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[1].String(),
		Version:          2,
	})
	require.NoError(t, err)
	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[2].String(),
		Version:          2,
	})
	require.NoError(t, err)

	res, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 60, res.VotingPower)
	require.EqualValues(t, 100, res.ThresholdPower)
	require.EqualValues(t, 120, res.TotalVotingPower)

	// remove one of the validators from the set
	delete(mockStakingKeeper.validators, testutil.ValAddrs[1].String())
	// the validator had 1 voting power, so we deduct it from the total
	mockStakingKeeper.totalVotingPower = mockStakingKeeper.totalVotingPower.SubRaw(1)

	res, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 59, res.VotingPower)
	require.EqualValues(t, 100, res.ThresholdPower)
	require.EqualValues(t, 119, res.TotalVotingPower)

	// That validator should not be able to signal a version
	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[1].String(),
		Version:          2,
	})
	require.Error(t, err)

	// resetting the tally should clear other votes
	upgradeKeeper.ResetTally(ctx)
	res, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 0, res.VotingPower)
}

// TestCanSkipVersion verifies that the signal keeper can upgrade to an app
// version greater than the next app version. Example: if the current version is
// 1, the next version is 2, but the chain can upgrade directly from 1 to 3.
func TestCanSkipVersion(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)
	require.Equal(t, v1.Version, ctx.BlockHeader().Version.App)

	validators := []sdk.ValAddress{
		testutil.ValAddrs[0],
		testutil.ValAddrs[1],
		testutil.ValAddrs[2],
		testutil.ValAddrs[3],
	}
	// signal version 3 for all validators
	for _, validator := range validators {
		_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
			ValidatorAddress: validator.String(),
			Version:          3,
		})
		require.NoError(t, err)
	}

	_, err := upgradeKeeper.TryUpgrade(ctx, &types.MsgTryUpgrade{})
	require.NoError(t, err)

	isUpgradePending := upgradeKeeper.IsUpgradePending(ctx)
	require.True(t, isUpgradePending)
}

func TestEmptyStore(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)

	res, err := upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 0, res.VotingPower)
	// 120 is the summation in voting power of the four validators
	require.EqualValues(t, 120, res.TotalVotingPower)
}

func TestThresholdVotingPower(t *testing.T) {
	upgradeKeeper, ctx, mockStakingKeeper := setup(t)

	for _, tc := range []struct {
		total     int64
		threshold int64
	}{
		{total: 1, threshold: 1},
		{total: 2, threshold: 2},
		{total: 3, threshold: 3},
		{total: 6, threshold: 5},
		{total: 59, threshold: 50},
	} {
		mockStakingKeeper.totalVotingPower = sdkmath.NewInt(tc.total)
		threshold, err := upgradeKeeper.GetVotingPowerThreshold(ctx)
		assert.NoError(t, err)
		require.EqualValues(t, tc.threshold, threshold.Int64())
	}
}

// TestResetTally verifies that ResetTally resets the VotingPower for all
// versions to 0 and any pending upgrade is cleared.
func TestResetTally(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)

	_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[0].String(), Version: 2})
	require.NoError(t, err)
	resp, err := upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 2})
	require.NoError(t, err)
	assert.Equal(t, uint64(40), resp.VotingPower)

	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[1].String(), Version: 3})
	require.NoError(t, err)
	resp, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 3})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), resp.VotingPower)

	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[2].String(), Version: 2})
	require.NoError(t, err)
	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[3].String(), Version: 2})
	require.NoError(t, err)

	_, err = upgradeKeeper.TryUpgrade(ctx, &types.MsgTryUpgrade{})
	require.NoError(t, err)

	assert.True(t, upgradeKeeper.IsUpgradePending(ctx))

	upgradeKeeper.ResetTally(ctx)

	resp, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 2})
	require.NoError(t, err)
	assert.Equal(t, uint64(0), resp.VotingPower)

	resp, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 3})
	require.NoError(t, err)
	assert.Equal(t, uint64(0), resp.VotingPower)

	assert.False(t, upgradeKeeper.IsUpgradePending(ctx))
}

func TestTryUpgrade(t *testing.T) {
	t.Run("should return an error if an upgrade is already pending", func(t *testing.T) {
		upgradeKeeper, ctx, _ := setup(t)

		_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[0].String(), Version: 2})
		require.NoError(t, err)
		_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[1].String(), Version: 2})
		require.NoError(t, err)
		_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[2].String(), Version: 2})
		require.NoError(t, err)
		_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[3].String(), Version: 2})
		require.NoError(t, err)

		// This TryUpgrade should succeed.
		_, err = upgradeKeeper.TryUpgrade(ctx, &types.MsgTryUpgrade{})
		require.NoError(t, err)

		// This TryUpgrade should fail because an upgrade is pending.
		_, err = upgradeKeeper.TryUpgrade(ctx, &types.MsgTryUpgrade{})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUpgradePending)
	})

	t.Run("should return an error if quorum version is less than or equal to the current version", func(t *testing.T) {
		upgradeKeeper, ctx, _ := setup(t)

		_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[0].String(), Version: 1})
		require.NoError(t, err)
		_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[1].String(), Version: 1})
		require.NoError(t, err)
		_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[2].String(), Version: 1})
		require.NoError(t, err)
		_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[3].String(), Version: 1})
		require.NoError(t, err)

		_, err = upgradeKeeper.TryUpgrade(ctx, &types.MsgTryUpgrade{})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidUpgradeVersion)
	})
}

func TestGetUpgrade(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)

	t.Run("should return an empty upgrade if no upgrade is pending", func(t *testing.T) {
		got, err := upgradeKeeper.GetUpgrade(ctx, &types.QueryGetUpgradeRequest{})
		require.NoError(t, err)
		assert.Nil(t, got.Upgrade)
	})

	t.Run("should return an upgrade if an upgrade is pending", func(t *testing.T) {
		_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[0].String(), Version: 2})
		require.NoError(t, err)
		_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[1].String(), Version: 2})
		require.NoError(t, err)
		_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[2].String(), Version: 2})
		require.NoError(t, err)
		_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[3].String(), Version: 2})
		require.NoError(t, err)

		// This TryUpgrade should succeed.
		_, err = upgradeKeeper.TryUpgrade(ctx, &types.MsgTryUpgrade{})
		require.NoError(t, err)

		got, err := upgradeKeeper.GetUpgrade(ctx, &types.QueryGetUpgradeRequest{})
		require.NoError(t, err)
		assert.Equal(t, v2.Version, got.Upgrade.AppVersion)
		assert.Equal(t, appconsts.UpgradeHeightDelay(appconsts.TestChainID, v2.Version), got.Upgrade.UpgradeHeight)
	})
}

func TestTallyAfterTryUpgrade(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)

	_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[0].String(),
		Version:          3,
	})
	require.NoError(t, err)

	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[1].String(),
		Version:          3,
	})
	require.NoError(t, err)

	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[2].String(),
		Version:          3,
	})
	require.NoError(t, err)

	_, err = upgradeKeeper.TryUpgrade(ctx, &types.MsgTryUpgrade{})
	require.NoError(t, err)

	// Previously there was a bug where querying for the version tally after a
	// successful try upgrade would result in a panic. See
	// https://github.com/celestiaorg/celestia-app/issues/4007
	res, err := upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 100, res.ThresholdPower)
	require.EqualValues(t, 120, res.TotalVotingPower)
}

func setup(t *testing.T) (signal.Keeper, sdk.Context, *mockStakingKeeper) {
	signalStore := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NoOpMetrics{})
	stateStore.MountStoreWithDB(signalStore, storetypes.StoreTypeIAVL, nil)
	require.NoError(t, stateStore.LoadLatestVersion())
	mockCtx := sdk.NewContext(stateStore, tmproto.Header{
		Version: cmtversion.Consensus{
			Block: 1,
			App:   1,
		},
	}, false, log.NewNopLogger()).WithHeaderInfo(header.Info{
		ChainID: appconsts.TestChainID, // TryUpgrade reads chainID from header info, not block header.
	})
	mockStakingKeeper := newMockStakingKeeper(
		map[string]int64{
			testutil.ValAddrs[0].String(): 40,
			testutil.ValAddrs[1].String(): 1,
			testutil.ValAddrs[2].String(): 59,
			testutil.ValAddrs[3].String(): 20,
		},
	)
	config := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	upgradeKeeper := signal.NewKeeper(config.Codec, signalStore, mockStakingKeeper)
	return upgradeKeeper, mockCtx, mockStakingKeeper
}

var _ signal.StakingKeeper = (*mockStakingKeeper)(nil)

type mockStakingKeeper struct {
	totalVotingPower sdkmath.Int
	validators       map[string]int64
}

func newMockStakingKeeper(validators map[string]int64) *mockStakingKeeper {
	totalVotingPower := sdkmath.NewInt(0)
	for _, power := range validators {
		totalVotingPower = totalVotingPower.AddRaw(power)
	}
	return &mockStakingKeeper{
		totalVotingPower: totalVotingPower,
		validators:       validators,
	}
}

func (m *mockStakingKeeper) GetLastTotalPower(_ context.Context) (sdkmath.Int, error) {
	return m.totalVotingPower, nil
}

func (m *mockStakingKeeper) GetLastValidatorPower(_ context.Context, addr sdk.ValAddress) (int64, error) {
	addrStr := addr.String()
	if power, ok := m.validators[addrStr]; ok {
		return power, nil
	}
	return 0, nil
}

func (m *mockStakingKeeper) GetValidator(_ context.Context, addr sdk.ValAddress) (validator stakingtypes.Validator, err error) {
	addrStr := addr.String()
	if _, ok := m.validators[addrStr]; ok {
		return stakingtypes.Validator{Status: stakingtypes.Bonded}, nil
	}
	return stakingtypes.Validator{}, stakingtypes.ErrNoValidatorFound
}
