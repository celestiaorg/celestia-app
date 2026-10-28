package keeper_test

import (
	"bytes"
	"math/big"
	"strings"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

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
			suite.Error(keeper.NewMigrator(*suite.keeper).Migrate1to2(suite.ctx))
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
