package keeper_test

import (
	"testing"

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
		err := hooks.AfterValidatorBeginUnbonding(ctx, sdk.ConsAddress{}, sdk.ValAddress{})
		assert.NoError(t, err)

		got := keeper.GetLatestUnBondingBlockHeight(ctx)
		assert.Equal(t, uint64(0), got)
	})
	t.Run("should set latest unboding height if app version is 1", func(t *testing.T) {
		ctx := sdk.NewContext(stateStore, tmproto.Header{Version: version.Consensus{App: 1}, Height: height}, false, log.NewNopLogger())
		err := hooks.AfterValidatorBeginUnbonding(ctx, sdk.ConsAddress{}, sdk.ValAddress{})
		assert.NoError(t, err)

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
		err := hooks.AfterValidatorCreated(ctx, valAddress)
		assert.NoError(t, err)

		address, ok := keeper.GetEVMAddress(ctx, valAddress)
		assert.False(t, ok)
		assert.Empty(t, address)
	})
	t.Run("should set EVM address if app version is 1", func(t *testing.T) {
		ctx := sdk.NewContext(stateStore, tmproto.Header{Version: version.Consensus{App: 1}, Height: height}, false, log.NewNopLogger())
		err := hooks.AfterValidatorCreated(ctx, valAddress)
		assert.NoError(t, err)

		address, ok := keeper.GetEVMAddress(ctx, valAddress)
		assert.True(t, ok)
		assert.Equal(t, common.HexToAddress("0x0000000000000000000076616C41646472657373"), address)
	})
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

type mockStakingKeeper struct{}

func newMockStakingKeeper(_ map[string]int64) *mockStakingKeeper {
	return &mockStakingKeeper{}
}

func (m *mockStakingKeeper) GetLastValidatorPower(_ sdk.Context, _ sdk.ValAddress) int64 {
	return 0
}

func (m *mockStakingKeeper) GetValidator(_ sdk.Context, _ sdk.ValAddress) (validator stakingtypes.Validator, found bool) {
	return stakingtypes.Validator{}, false
}

func (m *mockStakingKeeper) GetBondedValidatorsByPower(_ sdk.Context) []stakingtypes.Validator {
	return []stakingtypes.Validator{}
}
