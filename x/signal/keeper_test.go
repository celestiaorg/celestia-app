package signal_test

import (
	"fmt"
	"math"
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v2/x/signal"
	"github.com/celestiaorg/celestia-app/v2/x/signal/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
	tmdb "github.com/tendermint/tm-db"
)

const defaultUpgradeHeightDelay = int64(7 * 24 * 60 * 60 / 12) // 7 days * 24 hours * 60 minutes * 60 seconds / 12 seconds per block = 50,400 blocks.

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
			stakingKeeper := newMockStakingKeeper(tc.validators)
			k := signal.NewKeeper(nil, stakingKeeper)
			got := k.GetVotingPowerThreshold(sdk.Context{})
			assert.Equal(t, tc.want, got, fmt.Sprintf("want %v, got %v", tc.want.String(), got.String()))
		})
	}
}

func TestSignalVersion(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)
	goCtx := sdk.WrapSDKContext(ctx)
	t.Run("should return an error if the signal version is less than the current version", func(t *testing.T) {
		_, err := upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
			ValidatorAddress: testutil.ValAddrs[0].String(),
			Version:          0,
		})
		assert.Error(t, err)
		assert.ErrorIs(t, err, types.ErrInvalidVersion)
	})
	t.Run("should return an error if the signal version is greater than the next version", func(t *testing.T) {
		_, err := upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
			ValidatorAddress: testutil.ValAddrs[0].String(),
			Version:          3,
		})
		assert.Error(t, err)
		assert.ErrorIs(t, err, types.ErrInvalidVersion)
	})
	t.Run("should return an error if the validator was not found", func(t *testing.T) {
		_, err := upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
			ValidatorAddress: testutil.ValAddrs[4].String(),
			Version:          2,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, stakingtypes.ErrNoValidatorFound)
	})
	t.Run("should not return an error if the signal version and validator are valid", func(t *testing.T) {
		_, err := upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
			ValidatorAddress: testutil.ValAddrs[0].String(),
			Version:          2,
		})
		require.NoError(t, err)

		res, err := upgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
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
	goCtx := sdk.WrapSDKContext(ctx)
	_, err := upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[0].String(),
		Version:          0,
	})
	require.Error(t, err)
	_, err = upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[0].String(),
		Version:          3,
	})
	require.Error(t, err)

	_, err = upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[0].String(),
		Version:          2,
	})
	require.NoError(t, err)

	res, err := upgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 40, res.VotingPower)
	require.EqualValues(t, 100, res.ThresholdPower)
	require.EqualValues(t, 120, res.TotalVotingPower)

	_, err = upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[2].String(),
		Version:          2,
	})
	require.NoError(t, err)

	res, err = upgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 99, res.VotingPower)
	require.EqualValues(t, 100, res.ThresholdPower)
	require.EqualValues(t, 120, res.TotalVotingPower)

	_, err = upgradeKeeper.TryUpgrade(goCtx, &types.MsgTryUpgrade{})
	require.NoError(t, err)
	shouldUpgrade, version := upgradeKeeper.ShouldUpgrade(ctx)
	require.False(t, shouldUpgrade)
	require.Equal(t, uint64(0), version)

	// we now have 101/120
	_, err = upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[1].String(),
		Version:          2,
	})
	require.NoError(t, err)

	_, err = upgradeKeeper.TryUpgrade(goCtx, &types.MsgTryUpgrade{})
	require.NoError(t, err)

	shouldUpgrade, version = upgradeKeeper.ShouldUpgrade(ctx)
	require.False(t, shouldUpgrade)
	require.Equal(t, uint64(0), version)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + defaultUpgradeHeightDelay)

	shouldUpgrade, version = upgradeKeeper.ShouldUpgrade(ctx)
	require.True(t, shouldUpgrade)
	require.Equal(t, uint64(2), version)

	// update the version to 2
	ctx = ctx.WithBlockHeader(tmproto.Header{
		Version: tmversion.Consensus{
			Block: 1,
			App:   2,
		},
	})
	goCtx = sdk.WrapSDKContext(ctx)

	_, err = upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[0].String(),
		Version:          3,
	})
	require.NoError(t, err)

	res, err = upgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
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

	res, err = upgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 59, res.VotingPower)
	require.EqualValues(t, 100, res.ThresholdPower)
	require.EqualValues(t, 119, res.TotalVotingPower)

	// That validator should not be able to signal a version
	_, err = upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[1].String(),
		Version:          2,
	})
	require.Error(t, err)

	// resetting the tally should clear other votes
	upgradeKeeper.ResetTally(ctx)
	res, err = upgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 0, res.VotingPower)
}

func TestEmptyStore(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)
	goCtx := sdk.WrapSDKContext(ctx)

	res, err := upgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
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
		threshold := upgradeKeeper.GetVotingPowerThreshold(ctx)
		require.EqualValues(t, tc.threshold, threshold.Int64())
	}
}

// TestResetTally verifies that the VotingPower for all versions is reset to
// zero after calling ResetTally.
func TestResetTally(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)

	_, err := upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[0].String(), Version: 1})
	require.NoError(t, err)
	resp, err := upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 1})
	require.NoError(t, err)
	assert.Equal(t, uint64(40), resp.VotingPower)

	_, err = upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[1].String(), Version: 2})
	require.NoError(t, err)
	resp, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 2})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), resp.VotingPower)

	upgradeKeeper.ResetTally(ctx)

	resp, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 1})
	require.NoError(t, err)
	assert.Equal(t, uint64(0), resp.VotingPower)

	resp, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 2})
	require.NoError(t, err)
	assert.Equal(t, uint64(0), resp.VotingPower)
}

func setup(t *testing.T) (signal.Keeper, sdk.Context, *mockStakingKeeper) {
	signalStore := sdk.NewKVStoreKey(types.StoreKey)
	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(signalStore, storetypes.StoreTypeIAVL, nil)
	require.NoError(t, stateStore.LoadLatestVersion())
	mockCtx := sdk.NewContext(stateStore, tmproto.Header{
		Version: tmversion.Consensus{
			Block: 1,
			App:   1,
		},
	}, false, log.NewNopLogger())
	mockStakingKeeper := newMockStakingKeeper(
		map[string]int64{
			testutil.ValAddrs[0].String(): 40,
			testutil.ValAddrs[1].String(): 1,
			testutil.ValAddrs[2].String(): 59,
			testutil.ValAddrs[3].String(): 20,
		},
	)

	upgradeKeeper := signal.NewKeeper(signalStore, mockStakingKeeper)
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

func (m *mockStakingKeeper) GetLastTotalPower(_ sdk.Context) sdkmath.Int {
	return m.totalVotingPower
}

func (m *mockStakingKeeper) GetLastValidatorPower(_ sdk.Context, addr sdk.ValAddress) int64 {
	addrStr := addr.String()
	if power, ok := m.validators[addrStr]; ok {
		return power
	}
	return 0
}

func (m *mockStakingKeeper) GetValidator(_ sdk.Context, addr sdk.ValAddress) (validator stakingtypes.Validator, found bool) {
	addrStr := addr.String()
	if _, ok := m.validators[addrStr]; ok {
		return stakingtypes.Validator{Status: stakingtypes.Bonded}, true
	}
	return stakingtypes.Validator{}, false
}
