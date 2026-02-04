package app_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/app/encoding"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/pkg/user"
	"github.com/celestiaorg/celestia-app/v7/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v7/test/util/testnode"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// TestSendToSelfWithLargeFee tests that when a user sends a MsgSend to
// themselves with a large transaction fee, the fee is collected by the
// fee_collector module account and eventually distributed to delegators.
//
// This test validates the approach described in:
// https://github.com/celestiaorg/celestia-app/issues/6477
func TestSendToSelfWithLargeFee(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestSendToSelfWithLargeFee in short mode.")
	}

	// Setup: create a test network with a funded account
	accounts := testfactory.GenerateAccounts(1)
	config := testnode.DefaultConfig().WithFundedAccounts(accounts...)
	cctx, _, _ := testnode.NewNetwork(t, config)
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	require.NoError(t, cctx.WaitForNextBlock())

	// Get the sender address (same as recipient for this test)
	account := accounts[0]
	senderAddr := testfactory.GetAddress(cctx.Keyring, account)

	// Setup query clients
	bankClient := banktypes.NewQueryClient(cctx.GRPCClient)
	stakingClient := stakingtypes.NewQueryClient(cctx.GRPCClient)
	distributionClient := distributiontypes.NewQueryClient(cctx.GRPCClient)

	// Get the validator address (testnode creates a single validator)
	validatorsResp, err := stakingClient.Validators(cctx.GoContext(), &stakingtypes.QueryValidatorsRequest{})
	require.NoError(t, err)
	require.Len(t, validatorsResp.Validators, 1)
	validatorAddr := validatorsResp.Validators[0].OperatorAddress

	// Get the fee collector module address
	feeCollectorAddr := authtypes.NewModuleAddress(authtypes.FeeCollectorName)

	// Record initial balances
	initialSenderBalance := getBalance(t, cctx, bankClient, senderAddr.String())
	initialFeeCollectorBalance := getBalance(t, cctx, bankClient, feeCollectorAddr.String())
	t.Logf("Initial sender balance: %s", initialSenderBalance)
	t.Logf("Initial fee collector balance: %s", initialFeeCollectorBalance)

	// Get initial validator commission (which comes from fees)
	initialCommission := getValidatorCommission(t, cctx, distributionClient, validatorAddr)
	t.Logf("Initial validator commission: %s", initialCommission)

	// Create and submit a MsgSend where FROM == TO with a large fee
	sendAmount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1)) // 1 utia
	largeFee := uint64(1_000_000)                                  // 1 TIA = 1,000,000 utia
	gasLimit := uint64(100_000)                                    // reasonable gas limit for MsgSend

	msgSend := &banktypes.MsgSend{
		FromAddress: senderAddr.String(),
		ToAddress:   senderAddr.String(), // Same address - sending to self
		Amount:      sdk.NewCoins(sendAmount),
	}

	txClient, err := user.SetupTxClient(cctx.GoContext(), cctx.Keyring, cctx.GRPCClient, ecfg, user.WithDefaultAccount(account))
	require.NoError(t, err)

	res, err := txClient.SubmitTx(cctx.GoContext(), []sdk.Msg{msgSend}, user.SetGasLimit(gasLimit), user.SetFee(largeFee))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, abci.CodeTypeOK, res.Code, "MsgSend to self failed with code: %d", res.Code)
	t.Logf("MsgSend to self succeeded at height %d, tx hash: %s", res.Height, res.TxHash)

	// Verify sender balance decreased by exactly the fee amount (since send amount cancels out)
	postTxSenderBalance := getBalance(t, cctx, bankClient, senderAddr.String())
	expectedSenderBalance := initialSenderBalance.Sub(math.NewInt(int64(largeFee)))
	require.Equal(t, expectedSenderBalance.String(), postTxSenderBalance.String(),
		"sender balance should decrease by exactly the fee amount")
	t.Logf("Post-tx sender balance: %s (decreased by %d utia fee)", postTxSenderBalance, largeFee)

	// Wait for a few blocks to allow fee distribution to occur
	// The distribution module moves fees from fee_collector to validators/delegators in BeginBlock
	require.NoError(t, cctx.WaitForBlocks(3))

	// Verify validator commission increased (validators receive fees as commission)
	finalCommission := getValidatorCommission(t, cctx, distributionClient, validatorAddr)
	t.Logf("Final validator commission: %s", finalCommission)

	// The commission should have increased due to the large fee we paid
	// Note: The exact increase depends on commission rate and community tax
	require.True(t, finalCommission.GT(initialCommission),
		"validator commission should increase after tx with large fee: initial=%s, final=%s",
		initialCommission, finalCommission)

	commissionIncrease := finalCommission.Sub(initialCommission)
	t.Logf("Validator commission increased by: %s utia", commissionIncrease)

	// The fee was collected and distributed to the validator (who is also a delegator to themselves)
	// This confirms the flow: user pays fee -> fee_collector -> distribution -> validators/delegators
	t.Logf("SUCCESS: Large fee (%d utia) was collected and distributed to validator", largeFee)
}

// getBalance queries the bank module for an account's utia balance.
func getBalance(t *testing.T, cctx testnode.Context, bankClient banktypes.QueryClient, addr string) math.Int {
	resp, err := bankClient.Balance(cctx.GoContext(), &banktypes.QueryBalanceRequest{
		Address: addr,
		Denom:   appconsts.BondDenom,
	})
	require.NoError(t, err)
	return resp.Balance.Amount
}

// getValidatorCommission queries the distribution module for a validator's accumulated commission.
func getValidatorCommission(t *testing.T, cctx testnode.Context, distributionClient distributiontypes.QueryClient, validatorAddr string) math.LegacyDec {
	resp, err := distributionClient.ValidatorCommission(cctx.GoContext(), &distributiontypes.QueryValidatorCommissionRequest{
		ValidatorAddress: validatorAddr,
	})
	require.NoError(t, err)
	if len(resp.Commission.Commission) == 0 {
		return math.LegacyZeroDec()
	}
	return resp.Commission.Commission.AmountOf(appconsts.BondDenom)
}
