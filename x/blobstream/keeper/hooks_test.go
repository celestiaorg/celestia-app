package keeper_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	blobstreamkeeper "github.com/celestiaorg/celestia-app/v2/x/blobstream/keeper"
	blobstreamtypes "github.com/celestiaorg/celestia-app/v2/x/blobstream/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
	dbm "github.com/tendermint/tm-db"
)

func TestAfterValidatorBeginUnbonding(t *testing.T) {
	keeper, stateStore := setupKeeper(t)
	hooks := keeper.Hooks()
	height := int64(1)
	t.Run("should be a no-op if app version is 2", func(t *testing.T) {
		ctx := sdk.NewContext(stateStore, tmproto.Header{Version: version.Consensus{App: 2}, Height: height}, false, log.NewNopLogger())
		hooks.AfterValidatorBeginUnbonding(ctx, sdk.ConsAddress{}, sdk.ValAddress{})
		got := keeper.GetLatestUnBondingBlockHeight(ctx)
		assert.Equal(t, uint64(0), got)
	})
	t.Run("should set latest unboding height if app version is 1", func(t *testing.T) {
		ctx := sdk.NewContext(stateStore, tmproto.Header{Version: version.Consensus{App: 1}, Height: height}, false, log.NewNopLogger())
		hooks.AfterValidatorBeginUnbonding(ctx, sdk.ConsAddress{}, sdk.ValAddress{})
		got := keeper.GetLatestUnBondingBlockHeight(ctx)
		assert.Equal(t, uint64(height), got)
	})
}

func TestAfterValidatorCreated(t *testing.T) {
	keeper, stateStore := setupKeeper(t)
	hooks := keeper.Hooks()
	height := int64(1)
	valAddress := sdk.ValAddress([]byte("valAddress"))
	t.Run("should be a no-op if app version is 2", func(t *testing.T) {
		ctx := sdk.NewContext(stateStore, tmproto.Header{Version: version.Consensus{App: 2}, Height: height}, false, log.NewNopLogger())
		hooks.AfterValidatorCreated(ctx, valAddress)
		address, ok := keeper.GetEVMAddress(ctx, valAddress)
		assert.False(t, ok)
		assert.Empty(t, address)
	})
	t.Run("should set EVM address if app version is 1", func(t *testing.T) {
		ctx := sdk.NewContext(stateStore, tmproto.Header{Version: version.Consensus{App: 1}, Height: height}, false, log.NewNopLogger())
		hooks.AfterValidatorCreated(ctx, valAddress)
		address, ok := keeper.GetEVMAddress(ctx, valAddress)
		assert.True(t, ok)
		assert.Equal(t, common.HexToAddress("0x0000000000000000000076616C41646472657373"), address)
	})

}

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

func (m *mockStakingKeeper) GetBondedValidatorsByPower(ctx sdk.Context) []stakingtypes.Validator {
	return []stakingtypes.Validator{}
}

func setupKeeper(t *testing.T) (*blobstreamkeeper.Keeper, store.CommitMultiStore) {
	registry := codectypes.NewInterfaceRegistry()
	appCodec := codec.NewProtoCodec(registry)
	storeKey := sdk.NewKVStoreKey(blobstreamtypes.StoreKey)
	subspace := types.NewSubspace(appCodec, codec.NewLegacyAmino(), storeKey, storeKey, "params")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	stakingKeeper := newMockStakingKeeper(map[string]int64{})
	keeper := blobstreamkeeper.NewKeeper(
		appCodec,
		storeKey,
		subspace,
		stakingKeeper,
	)
	return keeper, stateStore
}
