package upgrade_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/x/upgrade"
	"github.com/celestiaorg/celestia-app/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
	tmdb "github.com/tendermint/tm-db"
)

func TestSignalQuorum(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)
	require.Equal(t, types.DefaultSignalQuorum, upgradeKeeper.SignalQuorum(ctx))
	require.EqualValues(t, 100, upgradeKeeper.GetVotingPowerThreshold(ctx).Int64())
	newParams := types.DefaultParams()
	newParams.SignalQuorum = types.MinSignalQuorum
	upgradeKeeper.SetParams(ctx, newParams)
	require.Equal(t, types.MinSignalQuorum, upgradeKeeper.SignalQuorum(ctx))
	require.EqualValues(t, 80, upgradeKeeper.GetVotingPowerThreshold(ctx).Int64())
}

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
	require.EqualValues(t, 30, res.VotingPower)
	require.EqualValues(t, 100, res.Threshold)
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
	require.EqualValues(t, 30, res.VotingPower)
	require.EqualValues(t, 100, res.Threshold)
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
	require.EqualValues(t, 80, res.VotingPower)
	require.EqualValues(t, 100, res.Threshold)
	require.EqualValues(t, 120, res.TotalVotingPower)

	upgradeKeeper.EndBlock(ctx)
	shouldUpgrade, version := upgradeKeeper.ShouldUpgrade()
	require.False(t, shouldUpgrade)
	require.Equal(t, uint64(0), version)

	// modify the quorum so we are right on the boundary 80/120 = 2/3
	newParams := types.DefaultParams()
	newParams.SignalQuorum = types.MinSignalQuorum
	upgradeKeeper.SetParams(ctx, newParams)

	upgradeKeeper.EndBlock(ctx)
	shouldUpgrade, version = upgradeKeeper.ShouldUpgrade()
	require.False(t, shouldUpgrade)
	require.Equal(t, uint64(0), version)
	require.EqualValues(t, 80, upgradeKeeper.GetVotingPowerThreshold(ctx).Int64())

	// we now have 81/120 = 0.675 > 2/3
	_, err = upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[1].String(),
		Version:          2,
	})
	require.NoError(t, err)

	upgradeKeeper.EndBlock(ctx)
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
	require.EqualValues(t, 51, res.VotingPower)
	require.EqualValues(t, 80, res.Threshold)
	require.EqualValues(t, 120, res.TotalVotingPower)

	// remove one of the validators from the set
	delete(mockStakingKeeper.validators, testutil.ValAddrs[2].String())
	mockStakingKeeper.totalVotingPower -= 50

	res, err = upgradeKeeper.VersionTally(goCtx, &types.QueryVersionTallyRequest{
		Version: 2,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, res.VotingPower)
	require.EqualValues(t, 47, res.Threshold)
	require.EqualValues(t, 70, res.TotalVotingPower)

	// That validator should not be able to signal a version
	_, err = upgradeKeeper.SignalVersion(goCtx, &types.MsgSignalVersion{
		ValidatorAddress: testutil.ValAddrs[2].String(),
		Version:          2,
	})
	require.Error(t, err)
}

func setup(t *testing.T) (upgrade.Keeper, sdk.Context, *mockStakingKeeper) {
	upgradeStore := sdk.NewKVStoreKey(types.StoreKey)
	paramStoreKey := sdk.NewKVStoreKey(paramtypes.StoreKey)
	tStoreKey := storetypes.NewTransientStoreKey(paramtypes.TStoreKey)
	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(paramStoreKey, storetypes.StoreTypeIAVL, nil)
	stateStore.MountStoreWithDB(tStoreKey, storetypes.StoreTypeTransient, nil)
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
			testutil.ValAddrs[0].String(): 30,
			testutil.ValAddrs[1].String(): 1,
			testutil.ValAddrs[2].String(): 50,
			testutil.ValAddrs[3].String(): 39,
		},
	}

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	paramsSubspace := paramtypes.NewSubspace(cdc,
		testutil.MakeTestCodec(),
		paramStoreKey,
		tStoreKey,
		types.ModuleName,
	)
	paramsSubspace.WithKeyTable(types.ParamKeyTable())
	upgradeKeeper := upgrade.NewKeeper(upgradeStore, 0, mockStakingKeeper, paramsSubspace)
	upgradeKeeper.SetParams(mockCtx, types.DefaultParams())
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
