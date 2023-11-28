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
	upgradeKeeper, ctx := setup(t)
	require.Equal(t, types.DefaultSignalQuorum, upgradeKeeper.SignalQuorum(ctx))
	require.EqualValues(t, 100, upgradeKeeper.GetVotingPowerThreshold(ctx).Int64())
	newParams := types.DefaultParams()
	newParams.SignalQuorum = types.MinSignalQuorum
	upgradeKeeper.SetParams(ctx, newParams)
	require.Equal(t, types.MinSignalQuorum, upgradeKeeper.SignalQuorum(ctx))
	require.EqualValues(t, 80, upgradeKeeper.GetVotingPowerThreshold(ctx).Int64())
}

func TestSignalling(t *testing.T) {
	upgradeKeeper, ctx := setup(t)
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
	require.EqualValues(t, 120, res.TotalVotingPower)
}

func setup(t *testing.T) (upgrade.Keeper, sdk.Context) {
	upgradeStore := sdk.NewKVStoreKey(types.StoreKey)
	paramStoreKey := sdk.NewKVStoreKey(paramtypes.StoreKey)
	tStoreKey := storetypes.NewTransientStoreKey(paramtypes.TStoreKey)
	val1 := testutil.ValAddrs[0]
	val2 := testutil.ValAddrs[1]
	val3 := testutil.ValAddrs[2]
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
	mockStakingKeeper := mockStakingKeeper{
		totalVotingPower: 120,
		validators: map[string]int64{
			val1.String(): 30,
			val2.String(): 40,
			val3.String(): 50,
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
	upgradeKeeper := upgrade.NewKeeper(upgradeStore, 0, &mockStakingKeeper, paramsSubspace)
	upgradeKeeper.SetParams(mockCtx, types.DefaultParams())
	return upgradeKeeper, mockCtx
}

var _ upgrade.StakingKeeper = (*mockStakingKeeper)(nil)

type mockStakingKeeper struct {
	totalVotingPower int64
	validators       map[string]int64
}

func (m *mockStakingKeeper) GetLastTotalPower(ctx sdk.Context) math.Int {
	return math.NewInt(m.totalVotingPower)
}

func (m *mockStakingKeeper) GetLastValidatorPower(ctx sdk.Context, addr sdk.ValAddress) int64 {
	addrStr := addr.String()
	if power, ok := m.validators[addrStr]; ok {
		return power
	}
	return 0
}

func (m *mockStakingKeeper) GetValidator(ctx sdk.Context, addr sdk.ValAddress) (validator stakingtypes.Validator, found bool) {
	addrStr := addr.String()
	if _, ok := m.validators[addrStr]; ok {
		return stakingtypes.Validator{Status: stakingtypes.Bonded}, true
	}
	return stakingtypes.Validator{}, false
}
