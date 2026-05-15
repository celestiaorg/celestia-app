package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/keeper"
	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

// testFixture is the in-memory keeper environment shared across this module's
// keeper tests. It wires a real ProtoCodec to an IAVL store backed by a memDB
// and seeds the gov module address as authority.
type testFixture struct {
	keeper    *keeper.Keeper
	ctx       sdk.Context
	cdc       codec.Codec
	authority string
}

// newTestFixture builds a keeper hooked to an isolated in-memory store. Each
// test gets a fresh fixture to keep state independent.
func newTestFixture(t *testing.T) testFixture {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	tkey := storetypes.NewTransientStoreKey("transient_test")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(tkey, storetypes.StoreTypeTransient, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	types.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	ctx := sdk.NewContext(stateStore, cmtproto.Header{Time: time.Now().UTC(), Height: 1}, false, log.NewNopLogger())
	k := keeper.NewKeeper(cdc, storeKey, authority)

	return testFixture{keeper: k, ctx: ctx, cdc: cdc, authority: authority}
}

// modifiedParams returns a Params value that is well-within all bounds but
// different from DefaultParams in every field. Useful as a round-trip target.
func modifiedParams() types.Params {
	return types.NewParams(
		1500*time.Millisecond, // TimeoutPropose       (default 3000)
		250*time.Millisecond,  // TimeoutProposeDelta  (default 500)
		1500*time.Millisecond, // TimeoutPrevote       (default 2000)
		250*time.Millisecond,  // TimeoutPrevoteDelta  (default 500)
		1500*time.Millisecond, // TimeoutPrecommit     (default 3000)
		250*time.Millisecond,  // TimeoutPrecommitDelta(default 500)
		300*time.Millisecond,  // TimeoutCommit        (default 500)
		1100*time.Millisecond, // DelayedPrecommitTimeout (default 2100)
	)
}

// TestGetParams_FallbackToDefaultsWhenUnset verifies that querying a fresh
// keeper (no SetParams invoked) returns DefaultParams.
func TestGetParams_FallbackToDefaultsWhenUnset(t *testing.T) {
	f := newTestFixture(t)
	require.Equal(t, types.DefaultParams(), f.keeper.GetParams(f.ctx))
}

// TestSetGetParams_RoundTrip writes modified params then reads them back.
func TestSetGetParams_RoundTrip(t *testing.T) {
	f := newTestFixture(t)
	want := modifiedParams()
	require.NoError(t, want.Validate(), "test fixture params should pass Validate")

	f.keeper.SetParams(f.ctx, want)
	got := f.keeper.GetParams(f.ctx)
	require.Equal(t, want, got)
}

// TestInitGenesis_WritesParams asserts InitGenesis persists the supplied
// genesis Params.
func TestInitGenesis_WritesParams(t *testing.T) {
	f := newTestFixture(t)
	want := modifiedParams()

	f.keeper.InitGenesis(f.ctx, *types.NewGenesisState(want))
	require.Equal(t, want, f.keeper.GetParams(f.ctx))
}

// TestExportGenesis_ReturnsCurrent asserts ExportGenesis reflects the current
// stored params.
func TestExportGenesis_ReturnsCurrent(t *testing.T) {
	f := newTestFixture(t)
	want := modifiedParams()
	f.keeper.SetParams(f.ctx, want)

	exported := f.keeper.ExportGenesis(f.ctx)
	require.NotNil(t, exported)
	require.Equal(t, want, exported.Params)
}
