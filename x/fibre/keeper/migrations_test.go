package keeper_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math/big"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Track the storage contract rather than heap measurements: no nested cache,
// bounded iterator reads, and no writes while an iterator is open.
type migrationMultiStore struct {
	storetypes.MultiStore
	store *migrationKVStore
}

func (s migrationMultiStore) GetKVStore(key storetypes.StoreKey) storetypes.KVStore {
	if key.Name() == types.StoreKey {
		return s.store
	}
	return s.MultiStore.GetKVStore(key)
}

func (s migrationMultiStore) CacheMultiStore() storetypes.CacheMultiStore {
	s.store.t.Fatal("migration must use the enclosing upgrade context, without another full-state cache")
	return nil
}

type migrationKVStore struct {
	storetypes.KVStore
	t      *testing.T
	open   int
	writes int
	gets   int
}

func (s *migrationKVStore) Get(key []byte) []byte {
	s.gets++
	return s.KVStore.Get(key)
}

func (s *migrationKVStore) Set(key, value []byte) {
	require.Zero(s.t, s.open, "close iterators before writing")
	s.writes++
	s.KVStore.Set(key, value)
}

func (s *migrationKVStore) Delete(key []byte) {
	require.Zero(s.t, s.open, "close iterators before deleting")
	s.writes++
	s.KVStore.Delete(key)
}

func (s *migrationKVStore) Iterator(start, end []byte) storetypes.Iterator {
	s.open++
	return &migrationIterator{Iterator: s.KVStore.Iterator(start, end), store: s}
}

type migrationIterator struct {
	storetypes.Iterator
	store *migrationKVStore
	reads int
}

func (i *migrationIterator) Next() {
	i.reads++
	require.LessOrEqual(i.store.t, i.reads, 128, "buffer at most one fixed-size batch")
	i.Iterator.Next()
}

func (i *migrationIterator) Close() error {
	i.store.open--
	return i.Iterator.Close()
}

func (suite *KeeperTestSuite) TestMigrateEscrowsBoundedCanonicalState() {
	suite.ctx, _ = suite.ctx.CacheContext()
	for n := range uint64(300) {
		addr := make([]byte, 20)
		binary.BigEndian.PutUint64(addr, n)
		signer := sdk.AccAddress(addr).String()
		balance := int64(n % 2)
		suite.keeper.SetEscrowAccount(suite.ctx, types.EscrowAccount{
			Signer: signer, Balance: sdk.NewInt64Coin("utia", balance), AvailableBalance: sdk.NewInt64Coin("utia", 0),
		})
		if balance > 0 {
			suite.keeper.SetWithdrawal(suite.ctx, types.Withdrawal{
				Signer: signer, Amount: sdk.NewInt64Coin("utia", balance),
				RequestedTimestamp: suite.ctx.BlockTime(), AvailableTimestamp: suite.ctx.BlockTime().Add(time.Hour),
			})
		}
	}
	store := &migrationKVStore{KVStore: suite.ctx.KVStore(suite.storeKey), t: suite.T()}
	ctx := suite.ctx.WithMultiStore(migrationMultiStore{MultiStore: suite.ctx.MultiStore(), store: store})
	suite.Require().NoError(keeper.NewMigrator(*suite.keeper).Migrate1to2(ctx))
	suite.Zero(store.open)
	suite.Zero(store.writes, "canonical accounts and withdrawals must not be rewritten")
	suite.Zero(store.gets, "validation must not retain canonical records in the SDK read cache")
}

func (suite *KeeperTestSuite) TestMigrateEscrowsAcrossBatches() {
	suite.ctx, _ = suite.ctx.CacheContext()
	refunds := make(map[string]int)
	suite.bankKeeper.(*MockBankKeeper).SendCoinsFromModuleToAccountFn = func(_ context.Context, module string, owner sdk.AccAddress, coins sdk.Coins) error {
		suite.Equal(types.ModuleName, module)
		suite.Equal(sdk.NewCoins(sdk.NewInt64Coin("stake", 20)), coins)
		refunds[owner.String()]++
		return nil
	}
	signers := make([]string, 0, 300)
	requested := suite.ctx.BlockTime()
	for n := range uint64(300) {
		addr := make([]byte, 20)
		binary.BigEndian.PutUint64(addr, n)
		lower := sdk.AccAddress(addr).String()
		signers = append(signers, lower)
		for index, signer := range []string{lower, strings.ToUpper(lower)} {
			denom, balance, reserved := "utia", int64(10), int64(1)
			if uint64(index) == n%2 {
				denom, balance, reserved = "stake", 20, 2
			}
			suite.keeper.SetEscrowAccount(suite.ctx, types.EscrowAccount{
				Signer: signer, Balance: sdk.NewInt64Coin(denom, balance), AvailableBalance: sdk.NewInt64Coin(denom, balance-reserved),
			})
			suite.keeper.SetWithdrawal(suite.ctx, types.Withdrawal{
				Signer: signer, Amount: sdk.NewInt64Coin(denom, reserved),
				RequestedTimestamp: requested, AvailableTimestamp: requested.Add(time.Hour),
			})
		}
	}
	store := &migrationKVStore{KVStore: suite.ctx.KVStore(suite.storeKey), t: suite.T()}
	ctx := suite.ctx.WithMultiStore(migrationMultiStore{MultiStore: suite.ctx.MultiStore(), store: store})
	suite.Require().NoError(keeper.NewMigrator(*suite.keeper).Migrate1to2(ctx))
	suite.Zero(store.open)
	for _, signer := range signers {
		account, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
		suite.Require().True(found)
		suite.Equal(sdk.NewInt64Coin("utia", 10), account.Balance)
		suite.Equal(sdk.NewInt64Coin("utia", 9), account.AvailableBalance)
		withdrawal, found := suite.keeper.GetWithdrawal(suite.ctx, signer, requested)
		suite.Require().True(found)
		suite.Equal(sdk.NewInt64Coin("utia", 1), withdrawal.Amount)
		suite.Equal(1, refunds[signer])
		_, found = suite.keeper.GetEscrowAccount(suite.ctx, strings.ToUpper(signer))
		suite.False(found)
		suite.Empty(suite.keeper.GetWithdrawalsBySigner(suite.ctx, strings.ToUpper(signer)))
	}
	genesis := suite.keeper.ExportGenesis(suite.ctx)
	suite.Len(genesis.Withdrawals, len(signers))
	suite.Require().NoError(genesis.Validate())
	suite.Require().NoError(keeper.NewMigrator(*suite.keeper).Migrate1to2(ctx))
	suite.Equal(genesis, suite.keeper.ExportGenesis(suite.ctx))
	for _, count := range refunds {
		suite.Equal(1, count, "retry must not refund twice")
	}
}

func (suite *KeeperTestSuite) TestMigrateEscrowsUppercaseOnly() {
	lower := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	upper := strings.ToUpper(lower)
	account := types.EscrowAccount{Signer: upper, Balance: sdk.NewInt64Coin("utia", 500), AvailableBalance: sdk.NewInt64Coin("utia", 400)}
	withdrawal := types.Withdrawal{
		Signer: upper, Amount: sdk.NewInt64Coin("utia", 100),
		RequestedTimestamp: suite.ctx.BlockTime(), AvailableTimestamp: suite.ctx.BlockTime().Add(time.Hour),
	}
	suite.keeper.SetEscrowAccount(suite.ctx, account)
	suite.keeper.SetWithdrawal(suite.ctx, withdrawal)
	payment := types.ProcessedPayment{PaymentPromiseHash: []byte{1, 2, 3}, ProcessedAt: suite.ctx.BlockTime()}
	suite.keeper.SetProcessedPayment(suite.ctx, payment)
	floor := suite.ctx.BlockTime().Add(-time.Hour)
	suite.keeper.SetPromiseFreshnessFloor(suite.ctx, floor)
	params := suite.keeper.GetParams(suite.ctx)
	suite.Require().NoError(keeper.NewMigrator(*suite.keeper).Migrate1to2(suite.ctx))

	account.Signer = lower
	got, found := suite.keeper.GetEscrowAccount(suite.ctx, lower)
	suite.True(found)
	suite.Equal(account, got)
	withdrawal.Signer = lower
	gotWithdrawal, found := suite.keeper.GetWithdrawal(suite.ctx, lower, withdrawal.RequestedTimestamp)
	suite.True(found)
	suite.Equal(withdrawal, gotWithdrawal)
	store := suite.ctx.KVStore(suite.storeKey)
	suite.False(store.Has(types.EscrowAccountKey(upper)))
	suite.False(store.Has(types.WithdrawalsBySignerKey(upper, withdrawal.RequestedTimestamp)))
	suite.False(store.Has(types.WithdrawalsByAvailableKey(withdrawal.AvailableTimestamp, withdrawal.RequestedTimestamp, upper)))
	suite.True(store.Has(types.WithdrawalsByAvailableKey(withdrawal.AvailableTimestamp, withdrawal.RequestedTimestamp, lower)))
	gotPayment, found := suite.keeper.GetProcessedPayment(suite.ctx, payment.PaymentPromiseHash)
	suite.True(found)
	suite.Equal(payment, gotPayment)
	gotFloor, err := suite.keeper.GetPromiseFreshnessFloor(suite.ctx)
	suite.Require().NoError(err)
	suite.True(floor.Equal(gotFloor))
	suite.Equal(params, suite.keeper.GetParams(suite.ctx))
}

func (suite *KeeperTestSuite) TestMigrateEscrowsRejectsInvalidState() {
	lower := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	for _, tc := range []struct {
		name   string
		mutate func()
	}{
		{"invalid address", func() {
			suite.keeper.SetEscrowAccount(suite.ctx, types.EscrowAccount{Signer: "invalid"})
		}},
		{"invalid balance", func() {
			suite.keeper.SetEscrowAccount(suite.ctx, types.EscrowAccount{Signer: lower, Balance: sdk.Coin{Denom: "utia"}})
		}},
		{"different balance denominations", func() {
			suite.keeper.SetEscrowAccount(suite.ctx, types.EscrowAccount{Signer: lower, Balance: sdk.NewInt64Coin("utia", 10), AvailableBalance: sdk.NewInt64Coin("stake", 10)})
		}},
		{"available exceeds total", func() {
			suite.keeper.SetEscrowAccount(suite.ctx, types.EscrowAccount{Signer: lower, Balance: sdk.NewInt64Coin("utia", 10), AvailableBalance: sdk.NewInt64Coin("utia", 11)})
		}},
		{"merge overflow", func() {
			amount := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
			for _, signer := range []string{lower, strings.ToUpper(lower)} {
				suite.keeper.SetEscrowAccount(suite.ctx, types.EscrowAccount{Signer: signer, Balance: sdk.NewCoin("utia", amount), AvailableBalance: sdk.NewInt64Coin("utia", 0)})
			}
		}},
		{"malformed protobuf", func() {
			suite.ctx.KVStore(suite.storeKey).Set(types.EscrowAccountKey(lower), []byte{0xff})
		}},
		{"key does not match signer", func() {
			account := types.EscrowAccount{Signer: lower, Balance: sdk.NewInt64Coin("utia", 10), AvailableBalance: sdk.NewInt64Coin("utia", 10)}
			suite.ctx.KVStore(suite.storeKey).Set(types.EscrowAccountKey(strings.ToUpper(lower)), suite.cdc.MustMarshal(&account))
		}},
		{"withdrawal denomination does not match escrow", func() {
			suite.keeper.SetWithdrawal(suite.ctx, types.Withdrawal{Signer: lower, Amount: sdk.NewInt64Coin("stake", 1), RequestedTimestamp: suite.ctx.BlockTime(), AvailableTimestamp: suite.ctx.BlockTime().Add(time.Hour)})
		}},
		{"orphan withdrawal", func() {
			suite.keeper.DeleteEscrowAccount(suite.ctx, lower)
			suite.keeper.SetWithdrawal(suite.ctx, types.Withdrawal{Signer: lower, Amount: sdk.NewInt64Coin("utia", 1), RequestedTimestamp: suite.ctx.BlockTime(), AvailableTimestamp: suite.ctx.BlockTime().Add(time.Hour)})
		}},
	} {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			suite.keeper.SetEscrowAccount(suite.ctx, types.EscrowAccount{Signer: lower, Balance: sdk.NewInt64Coin("utia", 10), AvailableBalance: sdk.NewInt64Coin("utia", 10)})
			tc.mutate()
			before := migrationStoreSnapshot(suite.ctx.KVStore(suite.storeKey))
			// Failed upgrades discard their enclosing block context.
			ctx, _ := suite.ctx.CacheContext()
			suite.Error(keeper.NewMigrator(*suite.keeper).Migrate1to2(ctx))
			suite.Equal(before, migrationStoreSnapshot(suite.ctx.KVStore(suite.storeKey)))
		})
	}
}

func migrationStoreSnapshot(store storetypes.KVStore) [][]byte {
	iterator := store.Iterator(nil, nil)
	defer iterator.Close()
	var entries [][]byte
	for ; iterator.Valid(); iterator.Next() {
		entries = append(entries, bytes.Clone(iterator.Key()), bytes.Clone(iterator.Value()))
	}
	return entries
}
