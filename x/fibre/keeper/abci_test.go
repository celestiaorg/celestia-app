package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
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

type ABCITestSuite struct {
	suite.Suite

	ctx        sdk.Context
	keeper     *keeper.Keeper
	msgServer  types.MsgServer
	cdc        codec.Codec
	bankKeeper *MockBankKeeper
}

func TestABCITestSuite(t *testing.T) {
	suite.Run(t, new(ABCITestSuite))
}

func (suite *ABCITestSuite) SetupTest() {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	tkey := storetypes.NewTransientStoreKey("transient_test")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(tkey, storetypes.StoreTypeTransient, nil)
	require.NoError(suite.T(), stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	suite.cdc = codec.NewProtoCodec(registry)

	suite.bankKeeper = &MockBankKeeper{}
	mockStakingKeeper := &MockStakingKeeper{}
	authority := authtypes.NewModuleAddress("gov").String()
	suite.ctx = sdk.NewContext(stateStore, cmtproto.Header{Time: time.Now().UTC()}, false, log.NewNopLogger())
	suite.keeper = keeper.NewKeeper(suite.cdc, storeKey, suite.bankKeeper, mockStakingKeeper, authority)
	suite.keeper.SetParams(suite.ctx, types.DefaultParams())
	suite.msgServer = keeper.NewMsgServerImpl(*suite.keeper)
}

func (suite *ABCITestSuite) TestBeginBlocker_ProcessAvailableWithdrawals() {
	// Create a test account
	privKey := secp256k1.GenPrivKey()
	signerAddr := sdk.AccAddress(privKey.PubKey().Address())
	signer := signerAddr.String()

	// Initial deposit
	depositAmount := sdk.NewCoin("utia", math.NewInt(1000000))

	// Deposit to escrow
	_, err := suite.msgServer.DepositToEscrow(suite.ctx, &types.MsgDepositToEscrow{
		Signer: signer,
		Amount: depositAmount,
	})
	suite.NoError(err)

	// Verify escrow account was created with correct balance
	escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	suite.Equal(depositAmount, escrowAccount.Balance)
	suite.Equal(depositAmount, escrowAccount.AvailableBalance)

	// Request withdrawal
	withdrawalAmount := sdk.NewCoin("utia", math.NewInt(500000))
	_, err = suite.msgServer.RequestWithdrawal(suite.ctx, &types.MsgRequestWithdrawal{
		Signer: signer,
		Amount: withdrawalAmount,
	})
	suite.NoError(err)

	// Verify available balance was decreased but total balance unchanged
	escrowAccount, found = suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	suite.Equal(depositAmount, escrowAccount.Balance)
	suite.Equal(depositAmount.Sub(withdrawalAmount), escrowAccount.AvailableBalance)

	// Advance time but not enough to process withdrawal
	params := suite.keeper.GetParams(suite.ctx)
	halfDelay := params.WithdrawalDelay / 2
	suite.ctx = suite.ctx.WithBlockTime(suite.ctx.BlockTime().Add(halfDelay))

	// Run BeginBlocker - should not process withdrawal yet
	err = suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify withdrawal still pending
	escrowAccount, found = suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	suite.Equal(depositAmount, escrowAccount.Balance)
	suite.Equal(depositAmount.Sub(withdrawalAmount), escrowAccount.AvailableBalance)

	// Advance time past withdrawal delay
	suite.ctx = suite.ctx.WithBlockTime(suite.ctx.BlockTime().Add(params.WithdrawalDelay))

	// Run BeginBlocker - should process withdrawal now
	err = suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify escrow account balance was decreased
	escrowAccount, found = suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	suite.Equal(depositAmount.Sub(withdrawalAmount), escrowAccount.Balance)
	suite.Equal(depositAmount.Sub(withdrawalAmount), escrowAccount.AvailableBalance)

	// Verify withdrawal was deleted from store
	withdrawals := suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
	suite.Empty(withdrawals)
}

func (suite *ABCITestSuite) TestBeginBlocker_ProcessMultipleWithdrawals() {
	// Create multiple test accounts
	amounts := []math.Int{
		math.NewInt(1000000),
		math.NewInt(2000000),
		math.NewInt(3000000),
	}

	signers := make([]string, 3)
	for i := range 3 {
		privKey := secp256k1.GenPrivKey()
		signerAddr := sdk.AccAddress(privKey.PubKey().Address())
		signers[i] = signerAddr.String()
	}

	// Deposit and request withdrawals for each account
	for i, signer := range signers {
		depositAmount := sdk.NewCoin("utia", amounts[i])

		_, err := suite.msgServer.DepositToEscrow(suite.ctx, &types.MsgDepositToEscrow{
			Signer: signer,
			Amount: depositAmount,
		})
		suite.NoError(err)

		withdrawalAmount := sdk.NewCoin("utia", amounts[i].QuoRaw(2))
		_, err = suite.msgServer.RequestWithdrawal(suite.ctx, &types.MsgRequestWithdrawal{
			Signer: signer,
			Amount: withdrawalAmount,
		})
		suite.NoError(err)

		// Add small delay between requests to test time ordering
		suite.ctx = suite.ctx.WithBlockTime(suite.ctx.BlockTime().Add(1 * time.Second))
	}

	// Advance time past withdrawal delay
	params := suite.keeper.GetParams(suite.ctx)
	suite.ctx = suite.ctx.WithBlockTime(suite.ctx.BlockTime().Add(params.WithdrawalDelay))

	// Run BeginBlocker - should process all withdrawals
	err := suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify all withdrawals were processed
	for i, signer := range signers {
		escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
		suite.True(found)
		expectedBalance := sdk.NewCoin("utia", amounts[i].QuoRaw(2))
		suite.Equal(expectedBalance, escrowAccount.Balance)
		suite.Equal(expectedBalance, escrowAccount.AvailableBalance)

		// Verify withdrawal was deleted
		withdrawals := suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
		suite.Empty(withdrawals)
	}
}

func (suite *ABCITestSuite) TestBeginBlocker_WithdrawalStaggeredTimes() {
	privKey := secp256k1.GenPrivKey()
	signerAddr := sdk.AccAddress(privKey.PubKey().Address())
	signer := signerAddr.String()

	// Deposit enough for multiple withdrawals
	depositAmount := sdk.NewCoin("utia", math.NewInt(3000000))

	_, err := suite.msgServer.DepositToEscrow(suite.ctx, &types.MsgDepositToEscrow{
		Signer: signer,
		Amount: depositAmount,
	})
	suite.NoError(err)

	// Request first withdrawal at time T
	withdrawal1 := sdk.NewCoin("utia", math.NewInt(1000000))
	time1 := suite.ctx.BlockTime()
	_, err = suite.msgServer.RequestWithdrawal(suite.ctx, &types.MsgRequestWithdrawal{
		Signer: signer,
		Amount: withdrawal1,
	})
	suite.NoError(err)

	// Request second withdrawal at time T+1h
	suite.ctx = suite.ctx.WithBlockTime(suite.ctx.BlockTime().Add(1 * time.Hour))
	withdrawal2 := sdk.NewCoin("utia", math.NewInt(1000000))
	time2 := suite.ctx.BlockTime()
	_, err = suite.msgServer.RequestWithdrawal(suite.ctx, &types.MsgRequestWithdrawal{
		Signer: signer,
		Amount: withdrawal2,
	})
	suite.NoError(err)

	// Advance to T+24h (first withdrawal becomes available)
	params := suite.keeper.GetParams(suite.ctx)
	suite.ctx = suite.ctx.WithBlockTime(time1.Add(params.WithdrawalDelay))

	// Run BeginBlocker - should process only first withdrawal
	err = suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify only first withdrawal processed
	escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	expectedBalance := depositAmount.Sub(withdrawal1)
	suite.Equal(expectedBalance, escrowAccount.Balance)
	expectedAvailable := expectedBalance.Sub(withdrawal2)
	suite.Equal(expectedAvailable, escrowAccount.AvailableBalance)

	// Verify one withdrawal still pending
	withdrawals := suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
	suite.Len(withdrawals, 1)

	// Advance to T+25h (second withdrawal becomes available)
	suite.ctx = suite.ctx.WithBlockTime(time2.Add(params.WithdrawalDelay))

	// Run BeginBlocker - should process second withdrawal
	err = suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify second withdrawal processed
	escrowAccount, found = suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	expectedBalance = expectedBalance.Sub(withdrawal2)
	suite.Equal(expectedBalance, escrowAccount.Balance)
	suite.Equal(expectedBalance, escrowAccount.AvailableBalance)

	// Verify all withdrawals processed
	withdrawals = suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
	suite.Empty(withdrawals)
}

func (suite *ABCITestSuite) TestBeginBlocker_WithdrawalDelayParamChange() {
	// This test verifies that withdrawals are processed correctly even if the
	// WithdrawalDelay parameter changes after the withdrawal is requested.
	// This is critical because we store the full Withdrawal struct (including
	// RequestedTimestamp) rather than computing it from availableAt - delay.

	privKey := secp256k1.GenPrivKey()
	signerAddr := sdk.AccAddress(privKey.PubKey().Address())
	signer := signerAddr.String()

	// Deposit funds
	depositAmount := sdk.NewCoin("utia", math.NewInt(2000000))
	_, err := suite.msgServer.DepositToEscrow(suite.ctx, &types.MsgDepositToEscrow{
		Signer: signer,
		Amount: depositAmount,
	})
	suite.NoError(err)

	// Request withdrawal with original delay (24 hours)
	withdrawalAmount := sdk.NewCoin("utia", math.NewInt(1000000))
	requestTime := suite.ctx.BlockTime()
	_, err = suite.msgServer.RequestWithdrawal(suite.ctx, &types.MsgRequestWithdrawal{
		Signer: signer,
		Amount: withdrawalAmount,
	})
	suite.NoError(err)

	// Verify withdrawal was created
	withdrawals := suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
	suite.Len(withdrawals, 1)
	suite.Equal(requestTime, withdrawals[0].RequestedTimestamp)

	// CHANGE THE PARAMETER: Update withdrawal delay to 48 hours
	params := suite.keeper.GetParams(suite.ctx)
	originalDelay := params.WithdrawalDelay
	params.WithdrawalDelay = 48 * time.Hour
	suite.keeper.SetParams(suite.ctx, params)

	// Advance time to original availableAt (T + 24h)
	// The withdrawal should still be processed because we stored the actual AvailableTimestamp
	suite.ctx = suite.ctx.WithBlockTime(requestTime.Add(originalDelay))

	// Run BeginBlocker - should process withdrawal even though current delay is 48h
	err = suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify withdrawal was processed successfully
	escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	suite.Equal(depositAmount.Sub(withdrawalAmount), escrowAccount.Balance)
	suite.Equal(depositAmount.Sub(withdrawalAmount), escrowAccount.AvailableBalance)

	// Verify withdrawal was deleted from store
	withdrawals = suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
	suite.Empty(withdrawals, "withdrawal should be deleted after processing")

	// Request another withdrawal with the new 48h delay
	withdrawal2Amount := sdk.NewCoin("utia", math.NewInt(500000))
	requestTime2 := suite.ctx.BlockTime()
	_, err = suite.msgServer.RequestWithdrawal(suite.ctx, &types.MsgRequestWithdrawal{
		Signer: signer,
		Amount: withdrawal2Amount,
	})
	suite.NoError(err)

	// Advance time by 24 hours - should NOT process (needs 48h now)
	suite.ctx = suite.ctx.WithBlockTime(requestTime2.Add(24 * time.Hour))
	err = suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify withdrawal still pending
	withdrawals = suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
	suite.Len(withdrawals, 1, "second withdrawal should still be pending after 24h")

	// Advance time by another 24 hours (total 48h) - should process now
	suite.ctx = suite.ctx.WithBlockTime(requestTime2.Add(48 * time.Hour))
	err = suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify second withdrawal was processed
	escrowAccount, found = suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	suite.Equal(depositAmount.Sub(withdrawalAmount).Sub(withdrawal2Amount), escrowAccount.Balance)

	// Verify all withdrawals processed
	withdrawals = suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
	suite.Empty(withdrawals, "all withdrawals should be processed")
}

func (suite *ABCITestSuite) TestBeginBlocker_InsufficientEscrowBalanceForSecondWithdrawal() {
	// This test verifies that if the escrow account's balance is insufficient
	// for a withdrawal during processing, the withdrawal is skipped but other
	// withdrawals continue to be processed.

	privKey := secp256k1.GenPrivKey()
	signerAddr := sdk.AccAddress(privKey.PubKey().Address())
	signer := signerAddr.String()

	// Deposit to escrow
	depositAmount := sdk.NewCoin("utia", math.NewInt(1000000))
	_, err := suite.msgServer.DepositToEscrow(suite.ctx, &types.MsgDepositToEscrow{
		Signer: signer,
		Amount: depositAmount,
	})
	suite.NoError(err)

	// Request first withdrawal (600000 utia)
	withdrawal1Amount := sdk.NewCoin("utia", math.NewInt(600000))
	_, err = suite.msgServer.RequestWithdrawal(suite.ctx, &types.MsgRequestWithdrawal{
		Signer: signer,
		Amount: withdrawal1Amount,
	})
	suite.NoError(err)

	// Advance time slightly to ensure different RequestedTimestamp for the second withdrawal
	suite.ctx = suite.ctx.WithBlockTime(suite.ctx.BlockTime().Add(1 * time.Second))

	// Request second withdrawal (400000 utia)
	// At this point, available balance is 400000 (1000000 - 600000)
	// But we can only request up to available balance
	withdrawal2Amount := sdk.NewCoin("utia", math.NewInt(400000))
	_, err = suite.msgServer.RequestWithdrawal(suite.ctx, &types.MsgRequestWithdrawal{
		Signer: signer,
		Amount: withdrawal2Amount,
	})
	suite.NoError(err)

	// Verify both withdrawals are pending
	withdrawals := suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
	suite.Len(withdrawals, 2)

	// Verify available balance was reduced to zero
	escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	suite.Equal(depositAmount, escrowAccount.Balance)
	suite.Equal(sdk.NewCoin("utia", math.NewInt(0)), escrowAccount.AvailableBalance)

	// Now manually reduce the escrow balance to simulate a situation where
	// the balance becomes insufficient. This shouldn't happen in practice.
	// This simulates an edge case where the actual balance is less than
	// the sum of pending withdrawals
	escrowAccount.Balance = sdk.NewCoin("utia", math.NewInt(700000))
	suite.keeper.SetEscrowAccount(suite.ctx, escrowAccount)

	// Advance time past withdrawal delay so both become available
	params := suite.keeper.GetParams(suite.ctx)
	suite.ctx = suite.ctx.WithBlockTime(suite.ctx.BlockTime().Add(params.WithdrawalDelay))

	// Run BeginBlocker - should process first withdrawal but skip second due to insufficient balance
	err = suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify first withdrawal was processed (balance decreased by withdrawal1Amount)
	escrowAccount, found = suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	expectedBalance := sdk.NewCoin("utia", math.NewInt(100000)) // 700000 - 600000
	suite.Equal(expectedBalance, escrowAccount.Balance)

	// Verify second withdrawal was NOT processed
	withdrawals = suite.keeper.GetWithdrawalsBySigner(suite.ctx, signer)
	suite.Len(withdrawals, 1, "second withdrawal should still be pending due to insufficient balance")
	suite.Equal(withdrawal2Amount, withdrawals[0].Amount)

	// The available balance should still be zero
	suite.Equal(sdk.NewCoin("utia", math.NewInt(0)), escrowAccount.AvailableBalance)
}

func (suite *ABCITestSuite) TestBeginBlocker_PruneProcessedPayments() {
	// Test that processed payments are pruned after the retention window

	// Create some processed payments at different times
	payment1Hash := []byte("payment-hash-1")
	payment2Hash := []byte("payment-hash-2")
	payment3Hash := []byte("payment-hash-3")

	baseTime := suite.ctx.BlockTime()

	// Payment 1: processed 30 days ago (should be pruned with 24h retention)
	payment1 := types.ProcessedPayment{
		PaymentPromiseHash: payment1Hash,
		ProcessedAt:        baseTime.Add(-30 * 24 * time.Hour),
	}
	suite.keeper.SetProcessedPayment(suite.ctx, payment1)

	// Payment 2: processed 12 hours ago (should NOT be pruned with 24h retention)
	payment2 := types.ProcessedPayment{
		PaymentPromiseHash: payment2Hash,
		ProcessedAt:        baseTime.Add(-12 * time.Hour),
	}
	suite.keeper.SetProcessedPayment(suite.ctx, payment2)

	// Payment 3: processed 25 hours ago (should be pruned with 24h retention)
	payment3 := types.ProcessedPayment{
		PaymentPromiseHash: payment3Hash,
		ProcessedAt:        baseTime.Add(-25 * time.Hour),
	}
	suite.keeper.SetProcessedPayment(suite.ctx, payment3)

	// Verify all payments are in the store
	_, found := suite.keeper.GetProcessedPayment(suite.ctx, payment1Hash)
	suite.True(found, "payment1 should be in store before pruning")
	_, found = suite.keeper.GetProcessedPayment(suite.ctx, payment2Hash)
	suite.True(found, "payment2 should be in store before pruning")
	_, found = suite.keeper.GetProcessedPayment(suite.ctx, payment3Hash)
	suite.True(found, "payment3 should be in store before pruning")

	// Run BeginBlocker - should prune payment1 and payment3
	err := suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify payment1 and payment3 were pruned
	_, found = suite.keeper.GetProcessedPayment(suite.ctx, payment1Hash)
	suite.False(found, "payment1 should be pruned (30 days old)")
	_, found = suite.keeper.GetProcessedPayment(suite.ctx, payment3Hash)
	suite.False(found, "payment3 should be pruned (25 hours old)")

	// Verify payment2 was NOT pruned
	_, found = suite.keeper.GetProcessedPayment(suite.ctx, payment2Hash)
	suite.True(found, "payment2 should NOT be pruned (12 hours old)")

	// Advance time by another 13 hours
	suite.ctx = suite.ctx.WithBlockTime(baseTime.Add(13 * time.Hour))

	// Run BeginBlocker again - should prune payment2 now
	err = suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify payment2 was pruned
	_, found = suite.keeper.GetProcessedPayment(suite.ctx, payment2Hash)
	suite.False(found, "payment2 should be pruned after advancing time")
}

func (suite *ABCITestSuite) TestBeginBlocker_PruneWithCustomRetentionWindow() {
	// Test pruning with a custom retention window

	// Set custom retention window to 1 hour
	params := suite.keeper.GetParams(suite.ctx)
	params.PaymentPromiseRetentionWindow = 1 * time.Hour
	suite.keeper.SetParams(suite.ctx, params)

	baseTime := suite.ctx.BlockTime()

	// Create payments at different times
	payment1Hash := []byte("payment-hash-1")
	payment2Hash := []byte("payment-hash-2")

	// Payment 1: processed 2 hours ago (should be pruned with 1h retention)
	payment1 := types.ProcessedPayment{
		PaymentPromiseHash: payment1Hash,
		ProcessedAt:        baseTime.Add(-2 * time.Hour),
	}
	suite.keeper.SetProcessedPayment(suite.ctx, payment1)

	// Payment 2: processed 30 minutes ago (should NOT be pruned with 1h retention)
	payment2 := types.ProcessedPayment{
		PaymentPromiseHash: payment2Hash,
		ProcessedAt:        baseTime.Add(-30 * time.Minute),
	}
	suite.keeper.SetProcessedPayment(suite.ctx, payment2)

	// Verify both payments are in the store
	_, found := suite.keeper.GetProcessedPayment(suite.ctx, payment1Hash)
	suite.True(found)
	_, found = suite.keeper.GetProcessedPayment(suite.ctx, payment2Hash)
	suite.True(found)

	// Run BeginBlocker - should prune only payment1
	err := suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify payment1 was pruned
	_, found = suite.keeper.GetProcessedPayment(suite.ctx, payment1Hash)
	suite.False(found, "payment1 should be pruned (2 hours old with 1h retention)")

	// Verify payment2 was NOT pruned
	_, found = suite.keeper.GetProcessedPayment(suite.ctx, payment2Hash)
	suite.True(found, "payment2 should NOT be pruned (30 minutes old with 1h retention)")
}

func (suite *ABCITestSuite) TestBeginBlocker_PruneEmptyState() {
	// Test that pruning works correctly when there are no processed payments

	// Run BeginBlocker on empty state
	err := suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err, "BeginBlocker should not error on empty state")
}

func (suite *ABCITestSuite) TestBeginBlocker_PruneAndWithdrawal() {
	// Test that both pruning and withdrawal processing work together in the same block

	// Create a test account for withdrawal
	privKey := secp256k1.GenPrivKey()
	signerAddr := sdk.AccAddress(privKey.PubKey().Address())
	signer := signerAddr.String()

	// Deposit to escrow
	depositAmount := sdk.NewCoin("utia", math.NewInt(1000000))
	_, err := suite.msgServer.DepositToEscrow(suite.ctx, &types.MsgDepositToEscrow{
		Signer: signer,
		Amount: depositAmount,
	})
	suite.NoError(err)

	// Request withdrawal
	withdrawalAmount := sdk.NewCoin("utia", math.NewInt(500000))
	_, err = suite.msgServer.RequestWithdrawal(suite.ctx, &types.MsgRequestWithdrawal{
		Signer: signer,
		Amount: withdrawalAmount,
	})
	suite.NoError(err)

	// Create an old processed payment that should be pruned
	oldPaymentHash := []byte("old-payment-hash")
	baseTime := suite.ctx.BlockTime()
	oldPayment := types.ProcessedPayment{
		PaymentPromiseHash: oldPaymentHash,
		ProcessedAt:        baseTime.Add(-30 * 24 * time.Hour),
	}
	suite.keeper.SetProcessedPayment(suite.ctx, oldPayment)

	// Advance time past withdrawal delay
	params := suite.keeper.GetParams(suite.ctx)
	suite.ctx = suite.ctx.WithBlockTime(suite.ctx.BlockTime().Add(params.WithdrawalDelay))

	// Run BeginBlocker - should both process withdrawal and prune payment
	err = suite.keeper.BeginBlocker(suite.ctx)
	suite.NoError(err)

	// Verify withdrawal was processed
	escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
	suite.True(found)
	suite.Equal(depositAmount.Sub(withdrawalAmount), escrowAccount.Balance)

	// Verify old payment was pruned
	_, found = suite.keeper.GetProcessedPayment(suite.ctx, oldPaymentHash)
	suite.False(found, "old payment should be pruned")
}
