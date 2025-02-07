package minfee_test

import (
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v4/x/minfee"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	paramkeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"
)

func TestNewModuleInitializesKeyTable(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(paramtypes.StoreKey)
	tStoreKey := storetypes.NewTransientStoreKey(paramtypes.TStoreKey)

	// Create the state store
	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NoOpMetrics{})
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(tStoreKey, storetypes.StoreTypeTransient, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()

	// Create a params keeper
	paramsKeeper := paramkeeper.NewKeeper(codec.NewProtoCodec(registry), codec.NewLegacyAmino(), storeKey, tStoreKey)
	subspace := paramsKeeper.Subspace(minfee.ModuleName)

	// Initialize the minfee module which registers the key table
	minfee.NewAppModule(paramsKeeper)

	// Require key table to be initialized
	hasKeyTable := subspace.HasKeyTable()
	require.True(t, hasKeyTable)
}
