package keeper_test

import (
	"context"
	"encoding/hex"
	"testing"

	"cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	cosmosstore "cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
)

// mockAccountKeeper implements types.AccountKeeper for testing
type mockAccountKeeper struct {
	moduleAddr sdk.AccAddress
}

func (m *mockAccountKeeper) GetModuleAddress(moduleName string) sdk.AccAddress {
	if moduleName == types.ModuleName {
		return m.moduleAddr
	}
	return nil
}

// mockBankKeeper implements types.BankKeeper for testing
type mockBankKeeper struct {
	balances map[string]sdk.Coins // address -> coins
}

func newMockBankKeeper() *mockBankKeeper {
	return &mockBankKeeper{
		balances: make(map[string]sdk.Coins),
	}
}

func (m *mockBankKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	coins, ok := m.balances[addr.String()]
	if !ok {
		return sdk.NewCoin(denom, math.ZeroInt())
	}
	return sdk.NewCoin(denom, coins.AmountOf(denom))
}

func (m *mockBankKeeper) GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	coins, ok := m.balances[addr.String()]
	if !ok {
		return sdk.Coins{}
	}
	return coins
}

func (m *mockBankKeeper) SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error {
	from := m.balances[fromAddr.String()]
	m.balances[fromAddr.String()] = from.Sub(amt...)

	to := m.balances[toAddr.String()]
	m.balances[toAddr.String()] = to.Add(amt...)
	return nil
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	from := m.balances[senderAddr.String()]
	m.balances[senderAddr.String()] = from.Sub(amt...)
	return nil
}

// SetBalance is a helper for setting up test state
func (m *mockBankKeeper) SetBalance(addr sdk.AccAddress, coins sdk.Coins) {
	m.balances[addr.String()] = coins
}

// TestKeeperSetup verifies keeper can be created with all dependencies.
// Skipped because NewKeeper requires a non-nil warpKeeper.
// Full keeper tests are in the integration test suite.
func TestKeeperSetup(t *testing.T) {
	t.Skip("requires warpKeeper - use integration tests")
}

// TestDeriveForwardingAddress verifies address derivation.
// The derivation is done via types.DeriveForwardingAddress, not a keeper method.
func TestDeriveForwardingAddress(t *testing.T) {
	destDomain := uint32(1)
	destRecipient := hexToBytes(t, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	addr, err := types.DeriveForwardingAddress(destDomain, destRecipient)
	require.NoError(t, err)
	require.NotNil(t, addr)
	require.Len(t, addr, 20)

	// Verify determinism
	addr2, err := types.DeriveForwardingAddress(destDomain, destRecipient)
	require.NoError(t, err)
	require.Equal(t, addr, addr2)
}

// NOTE: Tests for GetSetParams, FindHypTokenByDenom, and HasEnrolledRouter
// require proper proto generation or warp keeper mocks. These are covered
// by the integration test suite in test/interop/forwarding_integration_test.go

// setupKeeper creates a keeper with mocked dependencies for unit testing
func setupKeeper(t *testing.T) (keeper.Keeper, sdk.Context, *mockBankKeeper) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	db := tmdb.NewMemDB()
	stateStore := cosmosstore.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NoOpMetrics{})
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, nil)

	// Create mock keepers
	moduleAddr := sdk.AccAddress([]byte("forwarding_module__"))
	accountKeeper := &mockAccountKeeper{moduleAddr: moduleAddr}
	bankKeeper := newMockBankKeeper()

	// Create a store service wrapper
	storeService := runtime.NewKVStoreService(storeKey)

	// Create keeper (without warp keeper for unit tests - pass nil)
	// TIA token ID is now configured via params (set at genesis or via governance)
	k := keeper.NewKeeper(
		cdc,
		storeService,
		accountKeeper,
		bankKeeper,
		nil, // warpKeeper - nil for unit tests, use integration tests for warp functionality
	)

	// Note: We don't set default params here because the Params type requires
	// proper proto generation for collections serialization.
	// For tests that need params, use the integration test suite with proper setup.

	return k, ctx, bankKeeper
}

// kvStoreAdapter adapts storetypes.KVStore to store.KVStore interface
type kvStoreAdapter struct {
	store storetypes.KVStore
}

func (a *kvStoreAdapter) Get(key []byte) ([]byte, error) {
	return a.store.Get(key), nil
}

func (a *kvStoreAdapter) Has(key []byte) (bool, error) {
	return a.store.Has(key), nil
}

func (a *kvStoreAdapter) Set(key, value []byte) error {
	a.store.Set(key, value)
	return nil
}

func (a *kvStoreAdapter) Delete(key []byte) error {
	a.store.Delete(key)
	return nil
}

func (a *kvStoreAdapter) Iterator(start, end []byte) (store.Iterator, error) {
	return a.store.Iterator(start, end), nil
}

func (a *kvStoreAdapter) ReverseIterator(start, end []byte) (store.Iterator, error) {
	return a.store.ReverseIterator(start, end), nil
}

// Ensure kvStoreAdapter implements store.KVStore
var _ store.KVStore = (*kvStoreAdapter)(nil)

func hexToBytes(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}
