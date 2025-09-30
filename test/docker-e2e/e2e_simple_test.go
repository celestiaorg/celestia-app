package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/go-square/v3/share"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
)

func (s *CelestiaTestSuite) TestE2ESimple() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()

	cfg := dockerchain.DefaultConfig(s.client, s.network)
	celestia, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	s.Require().NoError(err, "failed to get chain")

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	// Cleanup resources when the test is done
	t.Cleanup(func() {
		if err := celestia.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	// Verify the chain is running
	height, err := celestia.Height(ctx)
	s.Require().NoError(err, "failed to get chain height")
	s.Require().Greater(height, int64(0), "chain height is zero")

	s.CreateTxSim(ctx, celestia)

	// Record start height for liveness check
	startHeight, err := celestia.Height(ctx)
	s.Require().NoError(err, "failed to get start height")

	assertTransactionsIncluded(ctx, t, celestia)

	testBankSend(t, celestia, cfg)

	s.T().Logf("Checking validator liveness from height %d", startHeight)
	s.Require().NoError(
		s.CheckLiveness(ctx, celestia),
		"validator liveness check failed",
	)
}

// assertTransactionsIncluded verifies that the required number of transactions have been included within a specified timeout.
func assertTransactionsIncluded(ctx context.Context, t *testing.T, celestia *tastoradockertypes.Chain) {
	pollCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	const requiredTxs = 10
	const pollInterval = 5 * time.Second

	networkInfo, err := celestia.GetNetworkInfo(ctx)
	if err != nil {
		t.Fatalf("Error getting network info: %v", err)
	}
	rpcAddress := "http://" + networkInfo.External.RPCAddress()
	// periodically check for transactions until timeout or required transactions are found
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check for transactions
			headers, err := testnode.ReadBlockchainHeaders(ctx, rpcAddress)
			if err != nil {
				t.Logf("Error reading blockchain headers: %v", err)
				continue
			}

			totalTxs := 0
			for _, blockMeta := range headers {
				totalTxs += blockMeta.NumTxs
			}

			t.Logf("Current transaction count: %d", totalTxs)

			if totalTxs >= requiredTxs {
				t.Logf("Found %d transactions, continuing with test", totalTxs)
				return
			}
		case <-pollCtx.Done():
			t.Fatalf("Timed out waiting for %d transactions", requiredTxs)
		}
	}
}

// testBankSend performs a basic bank send using txClient.
func testBankSend(t *testing.T, chain *tastoradockertypes.Chain, cfg *dockerchain.Config) {
	ctx := context.Background()

	// The key-ring stores wallets by name. Reusing a name causes
	// 'celestia-appd keys add' to fail with "key already exists", which would
	// break repeated or parallel test runs.  A timestamp keeps the name unique.
	recipientWalletName := fmt.Sprintf("recipient-%d", time.Now().UnixNano())

	// Create a new wallet with unique name
	wallet, err := chain.CreateWallet(ctx, recipientWalletName)
	require.NoError(t, err, "failed to create recipient wallet")
	recipientAddress := wallet.GetFormattedAddress()

	txClient, err := dockerchain.SetupTxClient(ctx, chain.Nodes()[0], cfg)
	require.NoError(t, err, "failed to setup TxClient")

	// get the default account address from TxClient (should be validator)
	fromAddr := txClient.DefaultAddress()
	toAddr, err := sdk.AccAddressFromBech32(recipientAddress)
	require.NoError(t, err, "failed to parse recipient address")

	t.Logf("Validator address: %s", fromAddr.String())
	t.Logf("Recipient address: %s", toAddr.String())

	sendAmount := sdk.NewCoins(sdk.NewCoin("utia", sdkmath.NewInt(1000000))) // 1 TIA
	msg := banktypes.NewMsgSend(fromAddr, toAddr, sendAmount)

	// Submit transaction using TxClient with proper minimum fee
	// Required: 0.025utia per gas unit, so 200000 * 0.025 = 5000 utia minimum
	txResp, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200000), user.SetFee(5000))
	require.NoError(t, err, "failed to submit transaction")
	require.Equal(t, uint32(0), txResp.Code, "transaction failed with code %d", txResp.Code)

	t.Logf("Transaction successful! TxHash: %s, Height: %d", txResp.TxHash, txResp.Height)

	// wait for additional blocks to ensure transaction is finalized
	err = wait.ForBlocks(ctx, 2, chain)
	require.NoError(t, err, "failed to wait for blocks after transaction")
}

// testPFBSubmission performs a basic PFB (Pay For Blob) submission using txClient.
func testPFBSubmission(t *testing.T, chain *tastoradockertypes.Chain, cfg *dockerchain.Config) {
	ctx := context.Background()

	txClient, err := dockerchain.SetupTxClient(ctx, chain.Nodes()[0], cfg)
	require.NoError(t, err, "failed to setup TxClient")

	ns := testfactory.RandomBlobNamespace()
	data := []byte(fmt.Sprintf("test blob data - %s", time.Now().Format(time.RFC3339)))

	blob, err := types.NewV0Blob(ns, data)
	require.NoError(t, err, "failed to create blob")

	t.Logf("Submitting PFB with namespace: %x, data length: %d", ns.Bytes(), len(data))

	// submit blob transaction using TxClient with proper minimum fee
	// Required: 0.025utia per gas unit, so 200000 * 0.025 = 5000 utia minimum
	txResp, err := txClient.SubmitPayForBlob(ctx, []*share.Blob{blob}, user.SetGasLimit(200000), user.SetFee(5000))
	require.NoError(t, err, "failed to submit PFB transaction")
	require.Equal(t, uint32(0), txResp.Code, "PFB transaction failed with code %d", txResp.Code)

	t.Logf("PFB transaction included on-chain. TxHash: %s, Height: %d", txResp.TxHash, txResp.Height)
}

// testVestingAminoTx performs a vesting account creation using amino-json encoding to test for compatibility issues.
// This test was added because amino-encoded vesting transactions were observed to break in certain version combinations.
func testVestingAminoTx(t *testing.T, chain *tastoradockertypes.Chain, cfg *dockerchain.Config) {
	ctx := context.Background()

	wallet, err := chain.CreateWallet(ctx, fmt.Sprintf("vesting-%d", time.Now().UnixNano()))
	require.NoError(t, err, "failed to create vesting wallet")

	txClient, err := dockerchain.SetupTxClient(ctx, chain.Nodes()[0], cfg)
	require.NoError(t, err, "failed to setup TxClient")

	// CRITICAL: Test amino-json encoding specifically (this is what fails between some older versions)
	// Create a separate amino-json signer to test the problematic encoding
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	keyring := cfg.Genesis.Keyring()
	chainID := cfg.Genesis.ChainID

	accounts, err := keyring.List()
	require.NoError(t, err, "failed to list keyring accounts")
	require.NotEmpty(t, accounts, "no accounts found in keyring")
	validatorAccount := accounts[0] // Use first account (validator)

	validatorAddr, err := validatorAccount.GetAddress()
	require.NoError(t, err, "failed to get validator address")

	toAddr, err := sdk.AccAddressFromBech32(wallet.GetFormattedAddress())
	require.NoError(t, err, "failed to parse vesting address")

	t.Logf("Creating amino-json vesting account from: %s to: %s", validatorAddr.String(), toAddr.String())

	vestingAmount := sdk.NewCoins(sdk.NewCoin("utia", sdkmath.NewInt(1000000)))
	startTime := time.Now().Add(1 * time.Hour).Unix()
	endTime := time.Now().Add(24 * time.Hour).Unix()

	msg := &vestingtypes.MsgCreateVestingAccount{
		FromAddress: validatorAddr.String(),
		ToAddress:   toAddr.String(),
		Amount:      vestingAmount,
		StartTime:   startTime,
		EndTime:     endTime,
		Delayed:     false,
	}

	rpcClient, err := chain.Nodes()[0].GetRPCClient()
	require.NoError(t, err, "failed to get RPC client")

	// Verify we're using the correct validator account (should match TxClient's default)
	defaultAddr := txClient.DefaultAddress()
	require.Equal(t, validatorAddr.String(), defaultAddr.String(), "validator addresses should match")

	// Get current sequence from TxClient's internal signer (which has been managing this account)
	// This is critical - we need the CURRENT sequence, not 0
	// Access the internal signer to get the current account state
	validatorAccountName := txClient.DefaultAccountName()
	internalAccount := txClient.Signer().Account(validatorAccountName)
	require.NotNil(t, internalAccount, "internal account should exist")

	currentSequence := internalAccount.Sequence()
	currentAccountNumber := internalAccount.AccountNumber()
	t.Logf("Using current account sequence: %d, account number: %d for amino signer", currentSequence, currentAccountNumber)

	account := user.NewAccount(validatorAccount.Name, currentAccountNumber, currentSequence)
	aminoSigner, err := user.NewSigner(keyring, encCfg.TxConfig, chainID, account)
	require.NoError(t, err, "failed to create amino signer")

	aminoSigner = aminoSigner.WithSignMode(signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON)

	txBytes, _, err := aminoSigner.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(200000), user.SetFee(5000))
	require.NoError(t, err, "amino-json vesting transaction creation failed - indicates version incompatibility")

	txResp, err := rpcClient.BroadcastTxSync(ctx, txBytes)
	require.NoError(t, err, "amino-json vesting transaction broadcast failed - indicates version incompatibility")
	require.Equal(t, uint32(0), txResp.Code, "amino-json vesting transaction failed with code %d - indicates version incompatibility", txResp.Code)

	t.Logf("Amino-json vesting transaction successful! TxHash: %s", txResp.Hash.String())
}
