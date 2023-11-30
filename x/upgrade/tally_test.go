package upgrade_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/x/upgrade"
	"github.com/celestiaorg/celestia-app/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
	tmdb "github.com/tendermint/tm-db"
)

func TestSignalVersion(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)
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
		ValidatorAddress: testutil.ValAddrs[4].String(),
		Version:          2,
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
	require.EqualValues(t, 100, res.VotingPower)
	require.EqualValues(t, 100, res.ThresholdPower)
	require.EqualValues(t, 120, res.TotalVotingPower)

	_, err = upgradeKeeper.TryUpgrade(goCtx, &types.MsgTryUpgrade{})
	require.NoError(t, err)
	shouldUpgrade, version := upgradeKeeper.ShouldUpgrade()
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
	shouldUpgrade, version = upgradeKeeper.ShouldUpgrade()
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
	require.EqualValues(t, 61, res.VotingPower)
	require.EqualValues(t, 100, res.ThresholdPower)
	require.EqualValues(t, 120, res.TotalVotingPower)

	// remove one of the validators from the set
	delete(mockStakingKeeper.validators, testutil.ValAddrs[1].String())
	mockStakingKeeper.totalVotingPower--

	res, err = upgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 60, res.VotingPower)
	require.EqualValues(t, 99, res.ThresholdPower)
	require.EqualValues(t, 119, res.TotalVotingPower)

	// That validator should not be able to signal a version
	_, err = upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[1].String(),
		Version:          2,
	})
	require.Error(t, err)
}

func setup(t *testing.T) (upgrade.Keeper, sdk.Context, *mockStakingKeeper) {
	upgradeStore := sdk.NewKVStoreKey(types.StoreKey)
	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(upgradeStore, storetypes.StoreTypeIAVL, nil)
	require.NoError(t, stateStore.LoadLatestVersion())
	mockCtx := sdk.NewContext(stateStore, tmproto.Header{
		Version: tmversion.Consensus{
			Block: 1,
			App:   1,
		},
	}, false, log.NewNopLogger())
	mockStakingKeeper := &mockStakingKeeper{
		totalVotingPower: 120,
		validators: map[string]int64{
			testutil.ValAddrs[0].String(): 40,
			testutil.ValAddrs[1].String(): 1,
			testutil.ValAddrs[2].String(): 60,
			testutil.ValAddrs[3].String(): 19,
		},
	}

	upgradeKeeper := upgrade.NewKeeper(upgradeStore, 0, mockStakingKeeper)
	return upgradeKeeper, mockCtx, mockStakingKeeper
}

var _ upgrade.StakingKeeper = (*mockStakingKeeper)(nil)

type mockStakingKeeper struct {
	totalVotingPower int64
	validators       map[string]int64
}

func (m *mockStakingKeeper) GetLastTotalPower(_ sdk.Context) math.Int {
	return math.NewInt(m.totalVotingPower)
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
