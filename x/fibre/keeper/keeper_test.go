package keeper_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx    sdk.Context
	keeper *keeper.Keeper
	cdc    codec.Codec
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) SetupTest() {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	tkey := storetypes.NewTransientStoreKey("transient_test")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(tkey, storetypes.StoreTypeTransient, nil)
	require.NoError(suite.T(), stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	suite.cdc = codec.NewProtoCodec(registry)

	mockBankKeeper := &MockBankKeeper{}
	authority := authtypes.NewModuleAddress("gov").String()
	suite.ctx = sdk.NewContext(stateStore, cmtproto.Header{Time: time.Now().UTC(), Height: 100}, false, nil)
	mockStakingKeeper := &MockStakingKeeper{}
	suite.keeper = keeper.NewKeeper(suite.cdc, storeKey, mockBankKeeper, mockStakingKeeper, authority)
	suite.keeper.SetParams(suite.ctx, types.DefaultParams())
}

func (suite *KeeperTestSuite) TestSetGetParams() {
	suite.T().Run("keeper should have default params", func(t *testing.T) {
		params := suite.keeper.GetParams(suite.ctx)
		suite.Equal(types.DefaultParams(), params)
	})

	suite.T().Run("keeper should set and get params", func(t *testing.T) {
		want := types.NewParams(
			2,            // GasPerBlobByte
			48*time.Hour, // WithdrawalDelay
			2*time.Hour,  // PaymentPromiseTimeout
			48*time.Hour, // PaymentPromiseRetentionWindow
			2000,         // PaymentPromiseHeightWindow
		)
		suite.keeper.SetParams(suite.ctx, want)
		got := suite.keeper.GetParams(suite.ctx)
		suite.Equal(want, got)
	})
}

func (suite *KeeperTestSuite) TestEscrowAccount() {
	signer := "celestia15drmhzw5kwgenvemy30rqqqgq52axf5wwrruf7"

	suite.T().Run("keeper should return false for non-existent account", func(t *testing.T) {
		_, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
		suite.False(found)
	})

	suite.T().Run("keeper should set and get account", func(t *testing.T) {
		want := types.EscrowAccount{
			Signer:           signer,
			Balance:          sdk.NewInt64Coin("utia", 1000),
			AvailableBalance: sdk.NewInt64Coin("utia", 800),
		}

		suite.keeper.SetEscrowAccount(suite.ctx, want)
		got, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)

		suite.True(found)
		suite.Equal(want.Signer, got.Signer)
		suite.Equal(want.Balance, got.Balance)
		suite.Equal(want.AvailableBalance, got.AvailableBalance)
	})

	suite.T().Run("keeper should delete account", func(t *testing.T) {
		suite.keeper.DeleteEscrowAccount(suite.ctx, signer)
		_, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
		suite.False(found)
	})
}

func (suite *KeeperTestSuite) TestWithdrawal() {
	signer := "celestia15drmhzw5kwgenvemy30rqqqgq52axf5wwrruf7"
	testTime := suite.ctx.BlockTime()

	suite.T().Run("keeper should return false for non-existent withdrawal", func(t *testing.T) {
		_, found := suite.keeper.GetWithdrawal(suite.ctx, signer, testTime)
		suite.False(found)
	})

	suite.T().Run("keeper should set and get withdrawal", func(t *testing.T) {
		params := suite.keeper.GetParams(suite.ctx)
		want := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 500),
			RequestedTimestamp: testTime,
			AvailableTimestamp: testTime.Add(params.WithdrawalDelay),
		}

		suite.keeper.SetWithdrawal(suite.ctx, want)
		got, found := suite.keeper.GetWithdrawal(suite.ctx, signer, testTime)

		suite.True(found)
		suite.Equal(want, got)
	})

	suite.T().Run("keeper should delete withdrawal", func(t *testing.T) {
		params := suite.keeper.GetParams(suite.ctx)
		withdrawal := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 500),
			RequestedTimestamp: testTime,
			AvailableTimestamp: testTime.Add(params.WithdrawalDelay),
		}
		suite.keeper.DeleteWithdrawal(suite.ctx, withdrawal)
		_, found := suite.keeper.GetWithdrawal(suite.ctx, signer, testTime)
		suite.False(found)
	})

	suite.T().Run("keeper should get withdrawals by signer", func(t *testing.T) {
		params := suite.keeper.GetParams(suite.ctx)
		requestedAt := testTime.Add(2 * time.Hour)
		want := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 100),
			RequestedTimestamp: requestedAt,
			AvailableTimestamp: requestedAt.Add(params.WithdrawalDelay),
		}
		suite.keeper.SetWithdrawal(suite.ctx, want)
		withdrawals := suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
		suite.Len(withdrawals, 1)
		suite.Equal(want, withdrawals[0])
	})

	suite.T().Run("keeper should parse withdrawals by available key", func(t *testing.T) {
		testSigner := "celestia15drmhzw5kwgenvemy30rqqqgq52axf5wwrruf7"
		testAvailableAt := testTime.Add(10 * time.Hour)

		// Create a key using the types function
		key := types.WithdrawalsByAvailableKey(testAvailableAt, testSigner)

		// Parse it back
		parsedTime, parsedSigner, err := suite.keeper.ParseWithdrawalsByAvailableKey(key)
		suite.NoError(err)

		// Verify parsed values match original
		suite.Equal(testAvailableAt, parsedTime, "parsed time should match original")
		suite.Equal(testSigner, parsedSigner, "parsed signer should match original")

		// Test with different signer (different length)
		testSigner2 := "celestia1abcdefghijklmnopqrstuvwxyz12345678901234"
		testAvailableAt2 := testTime.Add(20 * time.Hour)

		key2 := types.WithdrawalsByAvailableKey(testAvailableAt2, testSigner2)
		parsedTime2, parsedSigner2, err2 := suite.keeper.ParseWithdrawalsByAvailableKey(key2)
		suite.NoError(err2)
		suite.Equal(testAvailableAt2, parsedTime2, "parsed time should match original")
		suite.Equal(testSigner2, parsedSigner2, "parsed signer should match original")

		// Test that we can distinguish between different times
		suite.NotEqual(parsedTime, parsedTime2, "different times should parse differently")
		suite.NotEqual(parsedSigner, parsedSigner2, "different signers should parse differently")
	})

	suite.T().Run("keeper should get withdrawals by available timestamp", func(t *testing.T) {
		// Use unique timestamps to avoid conflicts with previous tests
		params := suite.keeper.GetParams(suite.ctx)
		baseTime := testTime.Add(100 * time.Hour) // Far in the future to avoid conflicts
		signer2 := "celestia1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx5wwrruf7"

		// Withdrawal 1: available earliest
		withdrawal1 := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 100),
			RequestedTimestamp: baseTime,
			AvailableTimestamp: baseTime.Add(params.WithdrawalDelay),
		}

		// Withdrawal 2: available in the middle
		withdrawal2 := types.Withdrawal{
			Signer:             signer2,
			Amount:             sdk.NewInt64Coin("utia", 200),
			RequestedTimestamp: baseTime.Add(1 * time.Hour),
			AvailableTimestamp: baseTime.Add(1 * time.Hour).Add(params.WithdrawalDelay),
		}

		// Withdrawal 3: available latest (should NOT be included)
		withdrawal3 := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 300),
			RequestedTimestamp: baseTime.Add(2 * time.Hour),
			AvailableTimestamp: baseTime.Add(2 * time.Hour).Add(params.WithdrawalDelay),
		}

		suite.keeper.SetWithdrawal(suite.ctx, withdrawal1)
		suite.keeper.SetWithdrawal(suite.ctx, withdrawal2)
		suite.keeper.SetWithdrawal(suite.ctx, withdrawal3)

		// Query for withdrawals available up to withdrawal2's time (inclusive)
		queryTime := withdrawal2.AvailableTimestamp
		iterator := suite.keeper.GetWithdrawalsByAvailableIterator(suite.ctx, queryTime)
		defer iterator.Close()

		// Should find withdrawal1 and withdrawal2, but not withdrawal3
		var foundWithdrawals []types.Withdrawal
		for ; iterator.Valid(); iterator.Next() {
			availableAt, signerFromKey, err := suite.keeper.ParseWithdrawalsByAvailableKey(iterator.Key())
			suite.NoError(err)

			// Skip if not one of our test withdrawals (from previous tests)
			if availableAt.Before(baseTime.Add(params.WithdrawalDelay)) {
				continue
			}

			suite.False(availableAt.After(queryTime), "withdrawal should be available before or at query time")

			var withdrawal types.Withdrawal
			suite.cdc.MustUnmarshal(iterator.Value(), &withdrawal)
			suite.Equal(signerFromKey, withdrawal.Signer, "signer from key should match withdrawal signer")
			foundWithdrawals = append(foundWithdrawals, withdrawal)
		}

		// Should have found exactly 2 withdrawals
		suite.Len(foundWithdrawals, 2, "should find withdrawals 1 and 2")
		suite.Equal(withdrawal1, foundWithdrawals[0], "first withdrawal should match")
		suite.Equal(withdrawal2, foundWithdrawals[1], "second withdrawal should match")
	})
}

func (suite *KeeperTestSuite) TestReduceWithdrawalsForPayment() {
	signer := "celestia15drmhzw5kwgenvemy30rqqqgq52axf5wwrruf7"
	params := suite.keeper.GetParams(suite.ctx)
	baseTime := suite.ctx.BlockTime().Add(200 * time.Hour) // Far in the future to avoid conflicts

	suite.T().Run("single withdrawal fully consumed", func(t *testing.T) {
		withdrawal := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 500),
			RequestedTimestamp: baseTime,
			AvailableTimestamp: baseTime.Add(params.WithdrawalDelay),
		}
		suite.keeper.SetWithdrawal(suite.ctx, withdrawal)

		err := suite.keeper.ReduceWithdrawalsForPayment(suite.ctx, signer, sdk.NewInt64Coin("utia", 500))
		suite.NoError(err)

		withdrawals := suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
		suite.Empty(withdrawals)
	})

	suite.T().Run("single withdrawal partially reduced", func(t *testing.T) {
		requestedAt := baseTime.Add(1 * time.Hour)
		withdrawal := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 500),
			RequestedTimestamp: requestedAt,
			AvailableTimestamp: requestedAt.Add(params.WithdrawalDelay),
		}
		suite.keeper.SetWithdrawal(suite.ctx, withdrawal)

		err := suite.keeper.ReduceWithdrawalsForPayment(suite.ctx, signer, sdk.NewInt64Coin("utia", 200))
		suite.NoError(err)

		got, found := suite.keeper.GetWithdrawal(suite.ctx, signer, requestedAt)
		suite.True(found)
		suite.Equal(sdk.NewInt64Coin("utia", 300), got.Amount)

		// cleanup
		suite.keeper.DeleteWithdrawal(suite.ctx, got)
	})

	suite.T().Run("multiple withdrawals consumed oldest first", func(t *testing.T) {
		requestedAt1 := baseTime.Add(2 * time.Hour)
		requestedAt2 := baseTime.Add(3 * time.Hour)
		w1 := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 300),
			RequestedTimestamp: requestedAt1,
			AvailableTimestamp: requestedAt1.Add(params.WithdrawalDelay),
		}
		w2 := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 400),
			RequestedTimestamp: requestedAt2,
			AvailableTimestamp: requestedAt2.Add(params.WithdrawalDelay),
		}
		suite.keeper.SetWithdrawal(suite.ctx, w1)
		suite.keeper.SetWithdrawal(suite.ctx, w2)

		// Consume 500: should fully consume w1 (300) and partially reduce w2 (200 remaining)
		err := suite.keeper.ReduceWithdrawalsForPayment(suite.ctx, signer, sdk.NewInt64Coin("utia", 500))
		suite.NoError(err)

		_, found := suite.keeper.GetWithdrawal(suite.ctx, signer, requestedAt1)
		suite.False(found, "first withdrawal should be fully consumed")

		got, found := suite.keeper.GetWithdrawal(suite.ctx, signer, requestedAt2)
		suite.True(found, "second withdrawal should still exist")
		suite.Equal(sdk.NewInt64Coin("utia", 200), got.Amount)

		// cleanup
		suite.keeper.DeleteWithdrawal(suite.ctx, got)
	})

	suite.T().Run("remaining zero is a no-op", func(t *testing.T) {
		requestedAt := baseTime.Add(4 * time.Hour)
		withdrawal := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 100),
			RequestedTimestamp: requestedAt,
			AvailableTimestamp: requestedAt.Add(params.WithdrawalDelay),
		}
		suite.keeper.SetWithdrawal(suite.ctx, withdrawal)

		err := suite.keeper.ReduceWithdrawalsForPayment(suite.ctx, signer, sdk.NewInt64Coin("utia", 0))
		suite.NoError(err)

		got, found := suite.keeper.GetWithdrawal(suite.ctx, signer, requestedAt)
		suite.True(found)
		suite.Equal(sdk.NewInt64Coin("utia", 100), got.Amount)

		// cleanup
		suite.keeper.DeleteWithdrawal(suite.ctx, got)
	})

	suite.T().Run("error when withdrawals do not cover shortfall", func(t *testing.T) {
		requestedAt := baseTime.Add(5 * time.Hour)
		withdrawal := types.Withdrawal{
			Signer:             signer,
			Amount:             sdk.NewInt64Coin("utia", 100),
			RequestedTimestamp: requestedAt,
			AvailableTimestamp: requestedAt.Add(params.WithdrawalDelay),
		}
		suite.keeper.SetWithdrawal(suite.ctx, withdrawal)

		err := suite.keeper.ReduceWithdrawalsForPayment(suite.ctx, signer, sdk.NewInt64Coin("utia", 200))
		suite.Error(err)
		suite.Contains(err.Error(), "do not cover the shortfall")
	})

	suite.T().Run("error when no withdrawals exist", func(t *testing.T) {
		unknownSigner := "celestia1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq0fr2sh"
		err := suite.keeper.ReduceWithdrawalsForPayment(suite.ctx, unknownSigner, sdk.NewInt64Coin("utia", 100))
		suite.Error(err)
		suite.Contains(err.Error(), "do not cover the shortfall")
	})
}

func (suite *KeeperTestSuite) TestProcessedPayment() {
	suite.T().Run("keeper should return false for non-existent processed payment", func(t *testing.T) {
		_, found := suite.keeper.GetProcessedPayment(suite.ctx, []byte("test-hash"))
		suite.False(found)
	})

	suite.T().Run("keeper should set and get processed payment", func(t *testing.T) {
		want := types.ProcessedPayment{
			PaymentPromiseHash: []byte("test-hash"),
			ProcessedAt:        suite.ctx.BlockTime(),
		}
		suite.keeper.SetProcessedPayment(suite.ctx, want)

		got, found := suite.keeper.GetProcessedPayment(suite.ctx, []byte("test-hash"))
		suite.True(found)
		suite.Equal(want, got)
	})

	suite.T().Run("keeper should delete processed payment", func(t *testing.T) {
		payment := types.ProcessedPayment{
			PaymentPromiseHash: []byte("test-hash"),
			ProcessedAt:        suite.ctx.BlockTime(),
		}
		suite.keeper.DeleteProcessedPayment(suite.ctx, payment)
		_, found := suite.keeper.GetProcessedPayment(suite.ctx, []byte("test-hash"))
		suite.False(found)
	})

	suite.T().Run("isPaymentProcessed should return false for non-existent payment promise", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.False(suite.keeper.IsPaymentPromiseProcessed(suite.ctx, &paymentPromise))
	})

	suite.T().Run("isPaymentProcessed should return true for existing payment promise", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		pp := fibre.PaymentPromise{}
		err := pp.FromProto(&paymentPromise)
		suite.NoError(err)
		paymentPromiseHash, err := pp.Hash()
		suite.NoError(err)

		suite.keeper.SetProcessedPayment(suite.ctx, types.ProcessedPayment{
			PaymentPromiseHash: paymentPromiseHash,
			ProcessedAt:        suite.ctx.BlockTime(),
		})

		suite.True(suite.keeper.IsPaymentPromiseProcessed(suite.ctx, &paymentPromise))
	})

	suite.T().Run("keeper should parse processed payments by time key", func(t *testing.T) {
		// Create a test time and payment promise hash
		testTime := suite.ctx.BlockTime()
		testHash := []byte("test-payment-hash")

		// Create the key using the helper function
		key := types.ProcessedPaymentsByTimeKey(testTime, testHash)

		// Parse the key
		parsedTime, parsedHash, err := suite.keeper.ParseProcessedPaymentsByTimeKey(key)
		suite.NoError(err)

		// Verify the parsed values match the originals
		suite.Equal(testTime, parsedTime, "parsed time should match original")
		suite.Equal(testHash, parsedHash, "parsed hash should match original")
	})

	suite.T().Run("keeper should get processed payments by time", func(t *testing.T) {
		// Create processed payments at different times
		baseTime := suite.ctx.BlockTime()

		payment1 := types.ProcessedPayment{
			PaymentPromiseHash: []byte("payment-hash-1"),
			ProcessedAt:        baseTime.Add(-2 * time.Hour),
		}
		payment2 := types.ProcessedPayment{
			PaymentPromiseHash: []byte("payment-hash-2"),
			ProcessedAt:        baseTime.Add(-1 * time.Hour),
		}
		payment3 := types.ProcessedPayment{
			PaymentPromiseHash: []byte("payment-hash-3"),
			ProcessedAt:        baseTime.Add(-30 * time.Minute),
		}
		payment4 := types.ProcessedPayment{
			PaymentPromiseHash: []byte("payment-hash-4"),
			ProcessedAt:        baseTime.Add(1 * time.Hour), // Future payment
		}

		// Set all payments
		suite.keeper.SetProcessedPayment(suite.ctx, payment1)
		suite.keeper.SetProcessedPayment(suite.ctx, payment2)
		suite.keeper.SetProcessedPayment(suite.ctx, payment3)
		suite.keeper.SetProcessedPayment(suite.ctx, payment4)

		// Get iterator for payments up to 45 minutes ago
		cutoffTime := baseTime.Add(-45 * time.Minute)
		iterator := suite.keeper.GetProcessedPaymentsByTimeIterator(suite.ctx, cutoffTime)
		defer iterator.Close()

		// Collect payments from iterator
		var foundPayments []types.ProcessedPayment
		for ; iterator.Valid(); iterator.Next() {
			processedAt, paymentPromiseHash, err := suite.keeper.ParseProcessedPaymentsByTimeKey(iterator.Key())
			suite.NoError(err)

			// Stop if we've reached payments within the retention window
			if processedAt.After(cutoffTime) {
				break
			}

			var payment types.ProcessedPayment
			suite.cdc.MustUnmarshal(iterator.Value(), &payment)
			suite.Equal(paymentPromiseHash, payment.PaymentPromiseHash, "hash from key should match payment")
			foundPayments = append(foundPayments, payment)
		}

		// Should have found exactly 2 payments (payment1 and payment2)
		suite.Len(foundPayments, 2, "should find payments 1 and 2 (older than 45 minutes)")
		suite.Equal(payment1, foundPayments[0], "first payment should match")
		suite.Equal(payment2, foundPayments[1], "second payment should match")
	})
}

func (suite *KeeperTestSuite) TestValidatePaymentPromiseInternal() {
	suite.T().Run("valid payment promise should pass validation", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.createEscrowAccount(paymentPromise)
		err := suite.keeper.ValidatePaymentPromiseInternal(suite.ctx, &paymentPromise)
		suite.NoError(err)
	})

	suite.T().Run("invalid payment promise format should fail", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		paymentPromise.Namespace = make([]byte, 10) // Invalid size (should be 29)
		err := suite.keeper.ValidatePaymentPromiseInternal(suite.ctx, &paymentPromise)
		suite.Error(err)
		suite.Contains(err.Error(), "invalid payment promise format")
	})

	suite.T().Run("invalid payment promise should fail validation", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		paymentPromise.BlobSize = 0 // Invalid: zero blob size

		err := suite.keeper.ValidatePaymentPromiseInternal(suite.ctx, &paymentPromise)
		suite.Error(err)
		suite.Contains(err.Error(), "upload size must be positive")
	})

	suite.T().Run("already processed payment promise should fail", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.createEscrowAccount(paymentPromise)

		// Mark payment promise as already processed
		pp := fibre.PaymentPromise{}
		err := pp.FromProto(&paymentPromise)
		suite.NoError(err)

		promiseHash, err := pp.Hash()
		suite.NoError(err)
		processedPayment := types.ProcessedPayment{
			PaymentPromiseHash: promiseHash,
			ProcessedAt:        suite.ctx.BlockTime(),
		}
		suite.keeper.SetProcessedPayment(suite.ctx, processedPayment)

		// Validate should fail because it's already processed
		err = suite.keeper.ValidatePaymentPromiseInternal(suite.ctx, &paymentPromise)
		suite.Error(err)
		suite.Contains(err.Error(), "payment promise has already been processed")
	})

	suite.T().Run("escrow account not found should fail", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()

		// Validate should fail because escrow account doesn't exist
		err := suite.keeper.ValidatePaymentPromiseInternal(suite.ctx, &paymentPromise)
		suite.Error(err)
		suite.Contains(err.Error(), "escrow account not found for signer")
	})

	suite.T().Run("insufficient balance should fail", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()

		signerAddr := sdk.AccAddress(paymentPromise.SignerPublicKey.Address())
		signerAddrStr := signerAddr.String()

		// Create escrow account with insufficient balance
		params := suite.keeper.GetParams(suite.ctx)
		gasRequired := uint64(paymentPromise.BlobSize) * uint64(params.GasPerBlobByte)
		requiredAmount := sdk.NewInt64Coin("utia", int64(gasRequired))
		insufficientBalance := sdk.NewInt64Coin("utia", int64(gasRequired)-1) // Less than required

		escrowAccount := types.EscrowAccount{
			Signer:           signerAddrStr,
			Balance:          insufficientBalance,
			AvailableBalance: insufficientBalance,
		}
		suite.keeper.SetEscrowAccount(suite.ctx, escrowAccount)

		// Validate should fail because of insufficient balance
		err := suite.keeper.ValidatePaymentPromiseInternal(suite.ctx, &paymentPromise)
		suite.Error(err)
		suite.Contains(err.Error(), "insufficient balance in escrow account")
		suite.Contains(err.Error(), fmt.Sprintf("required: %v", requiredAmount))
		suite.Contains(err.Error(), fmt.Sprintf("balance: %v", insufficientBalance))
	})
}

func (suite *KeeperTestSuite) TestValidatePaymentPromiseStateful() {
	suite.T().Run("payment promise with future creation timestamp should be accepted", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.createEscrowAccount(paymentPromise)

		// Set creation timestamp to the future
		paymentPromise.CreationTimestamp = suite.ctx.BlockTime().Add(1 * time.Hour)

		// Validate should fail because creation timestamp is in the future
		expirationTime, err := suite.keeper.ValidatePaymentPromiseStateful(suite.ctx, &paymentPromise)
		suite.NoError(err)
		wantTime := paymentPromise.CreationTimestamp.Add(suite.keeper.GetParams(suite.ctx).PaymentPromiseTimeout)
		suite.Equal(wantTime, expirationTime)
	})

	suite.T().Run("payment promise with timestamp before withdrawal delay should be rejected", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.createEscrowAccount(paymentPromise)

		params := suite.keeper.GetParams(suite.ctx)
		currentTime := suite.ctx.BlockTime()

		// Set creation timestamp to be older than (currentTime - withdrawalDelay)
		// This means it's too old and should be rejected
		paymentPromise.CreationTimestamp = currentTime.Add(-params.WithdrawalDelay).Add(-1 * time.Second)

		// Validate should fail because creation timestamp is too old
		_, err := suite.keeper.ValidatePaymentPromiseStateful(suite.ctx, &paymentPromise)
		suite.Error(err)
		suite.Contains(err.Error(), "creation_timestamp")
		suite.Contains(err.Error(), "must be greater than")
		suite.Contains(err.Error(), "current_time - withdrawal_delay")
	})

	suite.T().Run("payment promise with height within window should be accepted", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.createEscrowAccount(paymentPromise)

		currentHeight := suite.ctx.BlockHeight()
		// Set height to be within the window (e.g., 50 blocks back)
		paymentPromise.Height = currentHeight - 50

		_, err := suite.keeper.ValidatePaymentPromiseStateful(suite.ctx, &paymentPromise)
		suite.NoError(err)
	})

	suite.T().Run("payment promise with height too far in past should be rejected", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.createEscrowAccount(paymentPromise)

		params := suite.keeper.GetParams(suite.ctx)
		currentHeight := suite.ctx.BlockHeight()
		// Set height to be beyond the window
		paymentPromise.Height = currentHeight - int64(params.PaymentPromiseHeightWindow) - 1

		_, err := suite.keeper.ValidatePaymentPromiseStateful(suite.ctx, &paymentPromise)
		suite.Error(err)
		suite.Contains(err.Error(), "too far in the past")
	})

	suite.T().Run("payment promise with height more than 1 block ahead should be rejected", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.createEscrowAccount(paymentPromise)

		currentHeight := suite.ctx.BlockHeight()
		// Set height to be 2 blocks ahead
		paymentPromise.Height = currentHeight + 2

		_, err := suite.keeper.ValidatePaymentPromiseStateful(suite.ctx, &paymentPromise)
		suite.Error(err)
		suite.Contains(err.Error(), "too far in the future")
	})

	suite.T().Run("payment promise with height exactly at currentHeight + 1 should be accepted", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.createEscrowAccount(paymentPromise)

		currentHeight := suite.ctx.BlockHeight()
		// Set height to be exactly 1 block ahead
		paymentPromise.Height = currentHeight + 1

		_, err := suite.keeper.ValidatePaymentPromiseStateful(suite.ctx, &paymentPromise)
		suite.NoError(err)
	})

	suite.T().Run("payment promise with height at currentHeight should be accepted", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.createEscrowAccount(paymentPromise)

		currentHeight := suite.ctx.BlockHeight()
		// Set height to be exactly at current height
		paymentPromise.Height = currentHeight

		_, err := suite.keeper.ValidatePaymentPromiseStateful(suite.ctx, &paymentPromise)
		suite.NoError(err)
	})
}

func (suite *KeeperTestSuite) TestValidatePaymentPromiseStatefulForTimeout() {
	suite.T().Run("timeout mechanism should accept promise height outside window", func(t *testing.T) {
		paymentPromise := suite.createPaymentPromise()
		suite.createEscrowAccount(paymentPromise)

		params := suite.keeper.GetParams(suite.ctx)
		currentHeight := suite.ctx.BlockHeight()
		// Set height to be far beyond the window
		paymentPromise.Height = currentHeight - int64(params.PaymentPromiseHeightWindow) - 100

		// Set creation timestamp to be old enough that it's expired
		paymentPromise.CreationTimestamp = suite.ctx.BlockTime().Add(-params.PaymentPromiseTimeout).Add(-1 * time.Hour)

		// ValidatePaymentPromiseStatefulForTimeout should accept it (height validation is skipped)
		_, err := suite.keeper.ValidatePaymentPromiseStatefulForTimeout(suite.ctx, &paymentPromise)
		suite.NoError(err)
	})
}

// createPaymentPromise creates a properly signed and valid payment promise for testing
func (suite *KeeperTestSuite) createPaymentPromise() types.PaymentPromise {
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	signerPublicKey := *pubKey.(*secp256k1.PubKey)

	paymentPromise := types.PaymentPromise{
		ChainId:           "test-chain",
		Height:            int64(100),
		Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize)).Bytes(),
		BlobSize:          uint32(1000),
		BlobVersion:       0,
		Commitment:        make([]byte, 32),
		CreationTimestamp: time.Now().UTC().Truncate(time.Second),
		SignerPublicKey:   signerPublicKey,
		Signature:         make([]byte, 64),
	}

	paymentPromise = *suite.signPaymentPromise(&paymentPromise, privKey)
	return paymentPromise
}

// createEscrowAccount creates an escrow account for the given payment promise with sufficient balance
func (suite *KeeperTestSuite) createEscrowAccount(paymentPromise types.PaymentPromise) {
	signerAddr := sdk.AccAddress(paymentPromise.SignerPublicKey.Address())
	signerAddrStr := signerAddr.String()
	extraBalance := int64(1000)

	params := suite.keeper.GetParams(suite.ctx)
	gasRequired := uint64(paymentPromise.BlobSize) * uint64(params.GasPerBlobByte)
	availableBalance := sdk.NewInt64Coin("utia", int64(gasRequired)+extraBalance)

	escrowAccount := types.EscrowAccount{
		Signer:           signerAddrStr,
		Balance:          availableBalance,
		AvailableBalance: availableBalance,
	}
	suite.keeper.SetEscrowAccount(suite.ctx, escrowAccount)
}

func (suite *KeeperTestSuite) signPaymentPromise(paymentPromise *types.PaymentPromise, privKey *secp256k1.PrivKey) *types.PaymentPromise {
	pp := fibre.PaymentPromise{}
	err := pp.FromProto(paymentPromise)
	suite.NoError(err)

	signBytes, err := pp.SignBytes()
	suite.NoError(err)

	signature, err := privKey.Sign(signBytes)
	suite.NoError(err)
	paymentPromise.Signature = signature
	return paymentPromise
}
