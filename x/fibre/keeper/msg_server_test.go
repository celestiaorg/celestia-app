package keeper_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type MsgServerTestSuite struct {
	suite.Suite

	ctx           sdk.Context
	keeper        *keeper.Keeper
	msgServer     types.MsgServer
	cdc           codec.Codec
	bankKeeper    *MockBankKeeper
	stakingKeeper *MockStakingKeeper
	authority     string
}

func TestMsgServerTestSuite(t *testing.T) {
	suite.Run(t, new(MsgServerTestSuite))
}

func (suite *MsgServerTestSuite) SetupTest() {
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
	suite.stakingKeeper = &MockStakingKeeper{}
	suite.authority = authtypes.NewModuleAddress("gov").String()
	suite.ctx = sdk.NewContext(stateStore, cmtproto.Header{Time: time.Now().UTC(), Height: 100}, false, nil)
	suite.keeper = keeper.NewKeeper(suite.cdc, storeKey, suite.bankKeeper, suite.stakingKeeper, suite.authority)
	suite.keeper.SetParams(suite.ctx, types.DefaultParams())
	suite.msgServer = keeper.NewMsgServerImpl(*suite.keeper)
}

// TestDepositToEscrow tests the DepositToEscrow message handler
func (suite *MsgServerTestSuite) TestDepositToEscrow() {
	privKey := secp256k1.GenPrivKey()
	signerAddr := sdk.AccAddress(privKey.PubKey().Address())
	signer := signerAddr.String()
	depositAmount := sdk.NewInt64Coin(appconsts.BondDenom, 1000)

	suite.T().Run("successful deposit to new escrow account", func(t *testing.T) {
		msg := &types.MsgDepositToEscrow{
			Signer: signer,
			Amount: depositAmount,
		}

		resp, err := suite.msgServer.DepositToEscrow(suite.ctx, msg)
		suite.NoError(err)
		suite.NotNil(resp)

		// Verify escrow account was created with correct balance
		escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
		suite.True(found)
		suite.Equal(signer, escrowAccount.Signer)
		suite.Equal(depositAmount, escrowAccount.Balance)
		suite.Equal(depositAmount, escrowAccount.AvailableBalance)
	})

	suite.T().Run("successful deposit to existing escrow account", func(t *testing.T) {
		// Deposit again to the same account
		msg := &types.MsgDepositToEscrow{
			Signer: signer,
			Amount: depositAmount,
		}

		resp, err := suite.msgServer.DepositToEscrow(suite.ctx, msg)
		suite.NoError(err)
		suite.NotNil(resp)

		// Verify balance was increased
		escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
		suite.True(found)
		expectedBalance := depositAmount.Add(depositAmount)
		suite.Equal(expectedBalance, escrowAccount.Balance)
		suite.Equal(expectedBalance, escrowAccount.AvailableBalance)
	})

	suite.T().Run("invalid signer address", func(t *testing.T) {
		msg := &types.MsgDepositToEscrow{
			Signer: "invalid-address",
			Amount: depositAmount,
		}

		resp, err := suite.msgServer.DepositToEscrow(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "invalid signer address")
	})

	suite.T().Run("bank transfer failure", func(t *testing.T) {
		// Set up the mock to return an error
		originalSendCoins := suite.bankKeeper.SendCoinsFromAccountToModuleFn
		suite.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
			return sdkerrors.ErrInsufficientFunds
		}
		defer func() {
			suite.bankKeeper.SendCoinsFromAccountToModuleFn = originalSendCoins
		}()

		newPrivKey := secp256k1.GenPrivKey()
		newSignerAddr := sdk.AccAddress(newPrivKey.PubKey().Address())
		newSigner := newSignerAddr.String()

		msg := &types.MsgDepositToEscrow{
			Signer: newSigner,
			Amount: depositAmount,
		}

		resp, err := suite.msgServer.DepositToEscrow(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "failed to transfer funds to escrow")
	})
}

// TestRequestWithdrawal tests the RequestWithdrawal message handler
func (suite *MsgServerTestSuite) TestRequestWithdrawal() {
	privKey := secp256k1.GenPrivKey()
	signerAddr := sdk.AccAddress(privKey.PubKey().Address())
	signer := signerAddr.String()
	depositAmount := sdk.NewInt64Coin(appconsts.BondDenom, 1000)
	withdrawAmount := sdk.NewInt64Coin(appconsts.BondDenom, 500)

	// Setup: Create escrow account with balance
	escrowAccount := types.EscrowAccount{
		Signer:           signer,
		Balance:          depositAmount,
		AvailableBalance: depositAmount,
	}
	suite.keeper.SetEscrowAccount(suite.ctx, escrowAccount)

	suite.T().Run("successful withdrawal request", func(t *testing.T) {
		msg := &types.MsgRequestWithdrawal{
			Signer: signer,
			Amount: withdrawAmount,
		}

		resp, err := suite.msgServer.RequestWithdrawal(suite.ctx, msg)
		suite.NoError(err)
		suite.NotNil(resp)

		// Verify withdrawal was created
		withdrawal, found := suite.keeper.GetWithdrawal(suite.ctx, signer, suite.ctx.BlockTime())
		suite.True(found)
		suite.Equal(signer, withdrawal.Signer)
		suite.Equal(withdrawAmount, withdrawal.Amount)
		suite.Equal(suite.ctx.BlockTime(), withdrawal.RequestedTimestamp)

		// Verify available balance was decreased
		escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
		suite.True(found)
		suite.Equal(depositAmount, escrowAccount.Balance) // Balance unchanged
		expectedAvailable := depositAmount.Sub(withdrawAmount)
		suite.Equal(expectedAvailable, escrowAccount.AvailableBalance)
	})

	suite.T().Run("escrow account not found", func(t *testing.T) {
		nonExistentSigner := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
		msg := &types.MsgRequestWithdrawal{
			Signer: nonExistentSigner,
			Amount: withdrawAmount,
		}

		resp, err := suite.msgServer.RequestWithdrawal(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "escrow account not found for signer")
	})

	suite.T().Run("insufficient available balance", func(t *testing.T) {
		// Try to withdraw more than available balance
		excessiveAmount := sdk.NewInt64Coin(appconsts.BondDenom, 10000)
		msg := &types.MsgRequestWithdrawal{
			Signer: signer,
			Amount: excessiveAmount,
		}

		resp, err := suite.msgServer.RequestWithdrawal(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "insufficient available balance")
	})

	suite.T().Run("duplicate withdrawal request at same timestamp", func(t *testing.T) {
		// Setup: Create a new escrow account for this test
		duplicateTestSigner := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
		duplicateEscrowAccount := types.EscrowAccount{
			Signer:           duplicateTestSigner,
			Balance:          depositAmount,
			AvailableBalance: depositAmount,
		}
		suite.keeper.SetEscrowAccount(suite.ctx, duplicateEscrowAccount)

		// Make first withdrawal request
		firstMsg := &types.MsgRequestWithdrawal{
			Signer: duplicateTestSigner,
			Amount: withdrawAmount,
		}

		resp, err := suite.msgServer.RequestWithdrawal(suite.ctx, firstMsg)
		suite.NoError(err)
		suite.NotNil(resp)

		// Try to make a second withdrawal request at the same timestamp
		// (since we're in the same block, ctx.BlockTime() will be the same)
		secondMsg := &types.MsgRequestWithdrawal{
			Signer: duplicateTestSigner,
			Amount: withdrawAmount,
		}

		resp, err = suite.msgServer.RequestWithdrawal(suite.ctx, secondMsg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "withdrawal request already exists for signer")
	})
}

// TestPayForFibre tests the PayForFibre message handler
func (suite *MsgServerTestSuite) TestPayForFibre() {
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	signerPubKey := *pubKey.(*secp256k1.PubKey)
	signerAddr := sdk.AccAddress(pubKey.Address())
	signer := signerAddr.String()

	// Create a valid payment promise
	paymentPromise := suite.createPaymentPromise(signerPubKey, privKey)

	// Calculate required balance
	params := suite.keeper.GetParams(suite.ctx)
	gasRequired := uint64(paymentPromise.BlobSize) * uint64(params.GasPerBlobByte)
	requiredAmount := sdk.NewInt64Coin(appconsts.BondDenom, int64(gasRequired)+1000)

	// Setup: Create escrow account with sufficient balance
	escrowAccount := types.EscrowAccount{
		Signer:           signer,
		Balance:          requiredAmount,
		AvailableBalance: requiredAmount,
	}
	suite.keeper.SetEscrowAccount(suite.ctx, escrowAccount)

	// Setup: Create validator set for signature validation
	suite.setupValidatorSet()

	suite.T().Run("successful payment processing", func(t *testing.T) {
		msg := &types.MsgPayForFibre{
			Signer:              signer,
			PaymentPromise:      paymentPromise,
			ValidatorSignatures: suite.generateValidatorSignatures(&paymentPromise),
		}

		resp, err := suite.msgServer.PayForFibre(suite.ctx, msg)
		suite.NoError(err)
		suite.NotNil(resp)

		// Verify payment was processed
		pp := fibre.PaymentPromise{}
		err = pp.FromProto(&paymentPromise)
		suite.NoError(err)
		promiseHash, err := pp.Hash()
		suite.NoError(err)

		processedPayment, found := suite.keeper.GetProcessedPayment(suite.ctx, promiseHash)
		suite.True(found)
		suite.Equal(promiseHash, processedPayment.PaymentPromiseHash)

		// Verify balance was deducted
		escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
		suite.True(found)
		paymentAmount := sdk.NewInt64Coin(appconsts.BondDenom, int64(gasRequired))
		expectedBalance := requiredAmount.Sub(paymentAmount)
		suite.Equal(expectedBalance, escrowAccount.Balance)
		suite.Equal(expectedBalance, escrowAccount.AvailableBalance)
	})

	suite.T().Run("payment promise already processed", func(t *testing.T) {
		msg := &types.MsgPayForFibre{
			Signer:              signer,
			PaymentPromise:      paymentPromise,
			ValidatorSignatures: suite.generateValidatorSignatures(&paymentPromise),
		}

		resp, err := suite.msgServer.PayForFibre(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "payment promise has already been processed")
	})

	suite.T().Run("invalid payment promise signature", func(t *testing.T) {
		invalidPaymentPromise := suite.createPaymentPromise(signerPubKey, privKey)
		invalidPaymentPromise.Signature = make([]byte, 64) // Invalid signature

		msg := &types.MsgPayForFibre{
			Signer:              signer,
			PaymentPromise:      invalidPaymentPromise,
			ValidatorSignatures: suite.generateValidatorSignatures(&invalidPaymentPromise),
		}

		resp, err := suite.msgServer.PayForFibre(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "payment promise validation failed")
	})

	suite.T().Run("escrow account not found", func(t *testing.T) {
		newPrivKey := secp256k1.GenPrivKey()
		newPubKey := newPrivKey.PubKey()
		newSignerPubKey := *newPubKey.(*secp256k1.PubKey)
		newPaymentPromise := suite.createPaymentPromise(newSignerPubKey, newPrivKey)
		newSigner := sdk.AccAddress(newPubKey.Address()).String()

		msg := &types.MsgPayForFibre{
			Signer:              newSigner,
			PaymentPromise:      newPaymentPromise,
			ValidatorSignatures: suite.generateValidatorSignatures(&newPaymentPromise),
		}

		resp, err := suite.msgServer.PayForFibre(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "escrow account not found for signer")
	})

	suite.T().Run("insufficient balance", func(t *testing.T) {
		lowBalancePrivKey := secp256k1.GenPrivKey()
		lowBalancePubKey := lowBalancePrivKey.PubKey()
		lowBalanceSignerPubKey := *lowBalancePubKey.(*secp256k1.PubKey)
		lowBalanceSigner := sdk.AccAddress(lowBalancePubKey.Address()).String()
		lowBalancePaymentPromise := suite.createPaymentPromise(lowBalanceSignerPubKey, lowBalancePrivKey)

		// Create escrow account with insufficient balance
		insufficientBalance := sdk.NewInt64Coin(appconsts.BondDenom, 10)
		lowBalanceEscrowAccount := types.EscrowAccount{
			Signer:           lowBalanceSigner,
			Balance:          insufficientBalance,
			AvailableBalance: insufficientBalance,
		}
		suite.keeper.SetEscrowAccount(suite.ctx, lowBalanceEscrowAccount)

		msg := &types.MsgPayForFibre{
			Signer:              lowBalanceSigner,
			PaymentPromise:      lowBalancePaymentPromise,
			ValidatorSignatures: suite.generateValidatorSignatures(&lowBalancePaymentPromise),
		}

		resp, err := suite.msgServer.PayForFibre(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "insufficient balance")
	})
}

// TestPaymentPromiseTimeout tests the PaymentPromiseTimeout message handler
func (suite *MsgServerTestSuite) TestPaymentPromiseTimeout() {
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	signerPubKey := *pubKey.(*secp256k1.PubKey)
	signerAddr := sdk.AccAddress(pubKey.Address())
	signer := signerAddr.String()
	processor := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()

	// Create a payment promise with an old creation timestamp
	params := suite.keeper.GetParams(suite.ctx)
	oldTime := suite.ctx.BlockTime().Add(-params.PaymentPromiseTimeout).Add(-1 * time.Hour)
	paymentPromise := suite.createPaymentPromiseWithTime(signerPubKey, privKey, oldTime)

	// Calculate required balance
	gasRequired := uint64(paymentPromise.BlobSize) * uint64(params.GasPerBlobByte)
	requiredAmount := sdk.NewInt64Coin(appconsts.BondDenom, int64(gasRequired)+1000)

	// Setup: Create escrow account with sufficient balance
	escrowAccount := types.EscrowAccount{
		Signer:           signer,
		Balance:          requiredAmount,
		AvailableBalance: requiredAmount,
	}
	suite.keeper.SetEscrowAccount(suite.ctx, escrowAccount)

	suite.T().Run("successful timeout processing", func(t *testing.T) {
		msg := &types.MsgPaymentPromiseTimeout{
			Signer:         processor,
			PaymentPromise: paymentPromise,
		}

		resp, err := suite.msgServer.PaymentPromiseTimeout(suite.ctx, msg)
		suite.NoError(err)
		suite.NotNil(resp)

		// Verify payment was processed
		pp := fibre.PaymentPromise{}
		err = pp.FromProto(&paymentPromise)
		suite.NoError(err)
		promiseHash, err := pp.Hash()
		suite.NoError(err)

		processedPayment, found := suite.keeper.GetProcessedPayment(suite.ctx, promiseHash)
		suite.True(found)
		suite.Equal(promiseHash, processedPayment.PaymentPromiseHash)

		// Verify balance was deducted
		escrowAccount, found := suite.keeper.GetEscrowAccount(suite.ctx, signer)
		suite.True(found)
		paymentAmount := sdk.NewInt64Coin(appconsts.BondDenom, int64(gasRequired))
		expectedBalance := requiredAmount.Sub(paymentAmount)
		suite.Equal(expectedBalance, escrowAccount.Balance)
		suite.Equal(expectedBalance, escrowAccount.AvailableBalance)
	})

	suite.T().Run("payment promise not yet timed out", func(t *testing.T) {
		recentPrivKey := secp256k1.GenPrivKey()
		recentPubKey := recentPrivKey.PubKey()
		recentSignerPubKey := *recentPubKey.(*secp256k1.PubKey)
		recentSigner := sdk.AccAddress(recentPubKey.Address()).String()

		// Create payment promise with recent timestamp
		recentPaymentPromise := suite.createPaymentPromiseWithTime(recentSignerPubKey, recentPrivKey, suite.ctx.BlockTime())

		// Create escrow account
		recentEscrowAccount := types.EscrowAccount{
			Signer:           recentSigner,
			Balance:          requiredAmount,
			AvailableBalance: requiredAmount,
		}
		suite.keeper.SetEscrowAccount(suite.ctx, recentEscrowAccount)

		msg := &types.MsgPaymentPromiseTimeout{
			Signer:         processor,
			PaymentPromise: recentPaymentPromise,
		}

		resp, err := suite.msgServer.PaymentPromiseTimeout(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "payment promise has not yet timed out")
	})

	suite.T().Run("payment promise already processed", func(t *testing.T) {
		alreadyProcessedPrivKey := secp256k1.GenPrivKey()
		alreadyProcessedPubKey := alreadyProcessedPrivKey.PubKey()
		alreadyProcessedSignerPubKey := *alreadyProcessedPubKey.(*secp256k1.PubKey)
		alreadyProcessedSigner := sdk.AccAddress(alreadyProcessedPubKey.Address()).String()

		alreadyProcessedPaymentPromise := suite.createPaymentPromiseWithTime(alreadyProcessedSignerPubKey, alreadyProcessedPrivKey, oldTime)

		// Create escrow account
		alreadyProcessedEscrowAccount := types.EscrowAccount{
			Signer:           alreadyProcessedSigner,
			Balance:          requiredAmount,
			AvailableBalance: requiredAmount,
		}
		suite.keeper.SetEscrowAccount(suite.ctx, alreadyProcessedEscrowAccount)

		// Mark as already processed
		pp := fibre.PaymentPromise{}
		err := pp.FromProto(&alreadyProcessedPaymentPromise)
		suite.NoError(err)
		promiseHash, err := pp.Hash()
		suite.NoError(err)

		processedPayment := types.ProcessedPayment{
			PaymentPromiseHash: promiseHash,
			ProcessedAt:        suite.ctx.BlockTime(),
		}
		suite.keeper.SetProcessedPayment(suite.ctx, processedPayment)

		msg := &types.MsgPaymentPromiseTimeout{
			Signer:         processor,
			PaymentPromise: alreadyProcessedPaymentPromise,
		}

		resp, err := suite.msgServer.PaymentPromiseTimeout(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "payment promise has already been processed")
	})

	suite.T().Run("invalid payment promise", func(t *testing.T) {
		invalidPaymentPromise := suite.createPaymentPromiseWithTime(signerPubKey, privKey, oldTime)
		invalidPaymentPromise.Signature = make([]byte, 64) // Invalid signature

		msg := &types.MsgPaymentPromiseTimeout{
			Signer:         processor,
			PaymentPromise: invalidPaymentPromise,
		}

		resp, err := suite.msgServer.PaymentPromiseTimeout(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "payment promise validation failed")
	})

	suite.T().Run("insufficient balance in escrow account", func(t *testing.T) {
		insufficientPrivKey := secp256k1.GenPrivKey()
		insufficientPubKey := insufficientPrivKey.PubKey()
		insufficientSignerPubKey := *insufficientPubKey.(*secp256k1.PubKey)
		insufficientSigner := sdk.AccAddress(insufficientPubKey.Address()).String()

		insufficientPaymentPromise := suite.createPaymentPromiseWithTime(insufficientSignerPubKey, insufficientPrivKey, oldTime)

		// Create escrow account with insufficient total balance (but sufficient available balance)
		// This tests the defensive check added to prevent panic
		insufficientBalance := sdk.NewInt64Coin(appconsts.BondDenom, 10)
		insufficientEscrowAccount := types.EscrowAccount{
			Signer:           insufficientSigner,
			Balance:          insufficientBalance, // Very low balance
			AvailableBalance: insufficientBalance,
		}
		suite.keeper.SetEscrowAccount(suite.ctx, insufficientEscrowAccount)

		msg := &types.MsgPaymentPromiseTimeout{
			Signer:         processor,
			PaymentPromise: insufficientPaymentPromise,
		}

		resp, err := suite.msgServer.PaymentPromiseTimeout(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "insufficient balance")
	})
}

// TestUpdateFibreParams tests the UpdateFibreParams message handler
func (suite *MsgServerTestSuite) TestUpdateFibreParams() {
	suite.T().Run("successful params update", func(t *testing.T) {
		newParams := types.NewParams(
			5,            // GasPerBlobByte
			72*time.Hour, // WithdrawalDelay
			3*time.Hour,  // PaymentPromiseTimeout
			96*time.Hour, // PaymentPromiseRetentionWindow
			2000,         // PaymentPromiseHeightWindow
		)

		msg := &types.MsgUpdateFibreParams{
			Authority: suite.authority,
			Params:    newParams,
		}

		resp, err := suite.msgServer.UpdateFibreParams(suite.ctx, msg)
		suite.NoError(err)
		suite.NotNil(resp)

		// Verify params were updated
		updatedParams := suite.keeper.GetParams(suite.ctx)
		suite.Equal(newParams, updatedParams)
	})

	suite.T().Run("unauthorized signer", func(t *testing.T) {
		unauthorizedSigner := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
		newParams := types.DefaultParams()

		msg := &types.MsgUpdateFibreParams{
			Authority: unauthorizedSigner,
			Params:    newParams,
		}

		resp, err := suite.msgServer.UpdateFibreParams(suite.ctx, msg)
		suite.Error(err)
		suite.Nil(resp)
		suite.Contains(err.Error(), "invalid authority")
	})
}

// Helper functions

func (suite *MsgServerTestSuite) createPaymentPromise(signerPubKey secp256k1.PubKey, privKey *secp256k1.PrivKey) types.PaymentPromise {
	return suite.createPaymentPromiseWithTime(signerPubKey, privKey, suite.ctx.BlockTime())
}

func (suite *MsgServerTestSuite) createPaymentPromiseWithTime(signerPubKey secp256k1.PubKey, privKey *secp256k1.PrivKey, creationTime time.Time) types.PaymentPromise {
	paymentPromise := types.PaymentPromise{
		ChainId:           "test-chain",
		Height:            suite.ctx.BlockHeight(),
		Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize)).Bytes(),
		BlobSize:          uint32(1000),
		BlobVersion:       0,
		Commitment:        make([]byte, 32),
		CreationTimestamp: creationTime,
		SignerPublicKey:   signerPubKey,
		Signature:         make([]byte, 64),
	}

	// Sign the payment promise
	pp := fibre.PaymentPromise{}
	err := pp.FromProto(&paymentPromise)
	suite.NoError(err)

	signBytes, err := pp.SignBytes()
	suite.NoError(err)

	signature, err := privKey.Sign(signBytes)
	suite.NoError(err)
	paymentPromise.Signature = signature

	return paymentPromise
}

func (suite *MsgServerTestSuite) setupValidatorSet() {
	// Create a validator with ed25519 key
	valPrivKey := ed25519.GenPrivKey()
	valPubKey := valPrivKey.PubKey()

	// Create a validator
	val := stakingtypes.Validator{
		OperatorAddress: sdk.ValAddress(valPubKey.Address()).String(),
		ConsensusPubkey: nil, // Will be set below
		Tokens:          math.NewInt(1000000),
	}

	// Convert CometBFT pubkey to SDK pubkey
	pk, err := cryptocodec.FromCmtPubKeyInterface(valPubKey)
	suite.NoError(err)

	// Set consensus pubkey
	anyPubKey, err := codectypes.NewAnyWithValue(pk)
	suite.NoError(err)
	val.ConsensusPubkey = anyPubKey

	// Create historical info
	historicalInfo := stakingtypes.HistoricalInfo{
		Header: cmtproto.Header{
			Height: suite.ctx.BlockHeight(),
			Time:   suite.ctx.BlockTime(),
		},
		Valset: []stakingtypes.Validator{val},
	}

	// Store validator private key for signature generation
	suite.stakingKeeper.validatorKeys = map[int64]ed25519.PrivKey{
		suite.ctx.BlockHeight(): valPrivKey,
	}

	// Update mock to return this validator set
	suite.stakingKeeper.historicalInfo = map[int64]stakingtypes.HistoricalInfo{
		suite.ctx.BlockHeight(): historicalInfo,
	}
}

func (suite *MsgServerTestSuite) generateValidatorSignatures(paymentPromise *types.PaymentPromise) [][]byte {
	pp := fibre.PaymentPromise{}
	err := pp.FromProto(paymentPromise)
	suite.NoError(err)

	// Prepare validator sign bytes with domain separation (same as validation code)
	validatorSignBytes, err := pp.SignBytesValidator()
	suite.NoError(err)

	// Get validator key
	valPrivKey, ok := suite.stakingKeeper.validatorKeys[paymentPromise.Height]
	if !ok {
		return [][]byte{}
	}

	signature, err := valPrivKey.Sign(validatorSignBytes)
	suite.NoError(err)

	return [][]byte{signature}
}
