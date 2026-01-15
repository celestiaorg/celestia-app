package test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/app/encoding"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/pkg/user"
	"github.com/celestiaorg/celestia-app/v7/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v7/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v7/test/util/testnode"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/suite"
)

const (
	// utiaPerTIA is the number of utia in one TIA (1 TIA = 1,000,000 utia).
	utiaPerTIA = 1_000_000
	// halfTIA is half a TIA in utia, used for smaller test amounts.
	halfTIA = 500_000
)

// IntegrationTestSuite runs end-to-end tests against a real test network.
// It verifies the feeaddress module works correctly when integrated with the full app.
type IntegrationTestSuite struct {
	suite.Suite
	accounts []string         // funded test accounts
	cctx     testnode.Context // test network context with gRPC client
	ecfg     encoding.Config  // encoding config for tx building
}

func TestFeeAddressIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

// SetupSuite spins up a single-node test network with funded accounts.
func (s *IntegrationTestSuite) SetupSuite() {
	s.accounts = testfactory.GenerateAccounts(4)
	cfg := testnode.DefaultConfig().WithFundedAccounts(s.accounts...)
	cctx, _, _ := testnode.NewNetwork(s.T(), cfg)
	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
}

// TestFeeAddressSendAndForward verifies that sending utia to the fee address:
// 1. Tokens are transferred to fee address
// 2. Tokens are forwarded to fee collector by the EndBlocker
// 3. Fee address balance is empty after forwarding
func (s *IntegrationTestSuite) TestFeeAddressSendAndForward() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	// Setup: get test account and record initial balance
	account := s.accounts[0]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	sendAmount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(utiaPerTIA))
	initialBalance := s.getAccountBalance(accountAddr)

	// Build and submit MsgSend to fee address
	msgSend := &banktypes.MsgSend{
		FromAddress: accountAddr.String(),
		ToAddress:   feeaddresstypes.FeeAddressBech32,
		Amount:      sdk.NewCoins(sendAmount),
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgSend}, blobfactory.DefaultTxOpts()...)
	require.NoError(err)
	require.NotNil(res)
	require.Equal(abci.CodeTypeOK, res.Code, "send to fee address tx failed with code: %d", res.Code)

	// Wait for endblock to forward the tokens
	require.NoError(s.cctx.WaitForNextBlock())

	// Verify fee address is empty (EndBlocker forwarded tokens to fee collector)
	// Note: We can't check fee collector balance because the distribution module's
	// BeginBlocker distributes tokens to validators, emptying the fee collector.
	feeAddressBalance := s.getFeeAddressBalance()
	require.True(feeAddressBalance.IsZero(),
		"fee address should be empty after EndBlocker forwards tokens: balance=%s",
		feeAddressBalance)

	// Verify account balance decreased by at least send amount (gas fees cause additional decrease)
	finalBalance := s.getAccountBalance(accountAddr)
	require.True(finalBalance.LT(initialBalance.Sub(sendAmount.Amount)),
		"account balance should decrease by at least send amount: initial=%s, final=%s, sendAmount=%s",
		initialBalance, finalBalance, sendAmount.Amount)
}

// TestFeeAddressRejectsNonUtia verifies that sending non-utia tokens to the fee address
// is rejected by the ante handler.
func (s *IntegrationTestSuite) TestFeeAddressRejectsNonUtia() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[1]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	// Try to send a different denomination to the fee address
	wrongDenomAmount := sdk.NewCoin("wrongdenom", math.NewInt(1000000))

	msgSend := &banktypes.MsgSend{
		FromAddress: accountAddr.String(),
		ToAddress:   feeaddresstypes.FeeAddressBech32,
		Amount:      sdk.NewCoins(wrongDenomAmount),
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	// This should fail during ante handler (fee address restriction)
	_, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgSend}, blobfactory.DefaultTxOpts()...)
	require.Error(err, "sending non-utia to fee address should fail")
}

// TestMsgMultiSendToFeeAddress verifies that MsgMultiSend to the fee address
// correctly forwards tokens to the fee collector.
func (s *IntegrationTestSuite) TestMsgMultiSendToFeeAddress() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[2]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	sendAmount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(halfTIA))
	initialBalance := s.getAccountBalance(accountAddr)

	// Build and submit MsgMultiSend to fee address
	msgMultiSend := &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{
			{Address: accountAddr.String(), Coins: sdk.NewCoins(sendAmount)},
		},
		Outputs: []banktypes.Output{
			{Address: feeaddresstypes.FeeAddressBech32, Coins: sdk.NewCoins(sendAmount)},
		},
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgMultiSend}, blobfactory.DefaultTxOpts()...)
	require.NoError(err)
	require.NotNil(res)
	require.Equal(abci.CodeTypeOK, res.Code, "MsgMultiSend to fee address tx failed with code: %d", res.Code)

	// Wait for EndBlocker to forward the tokens
	require.NoError(s.cctx.WaitForNextBlock())

	// Verify fee address is empty (EndBlocker forwarded tokens to fee collector)
	feeAddressBalance := s.getFeeAddressBalance()
	require.True(feeAddressBalance.IsZero(),
		"fee address should be empty after EndBlocker forwards tokens: balance=%s",
		feeAddressBalance)

	// Verify account balance decreased by at least send amount
	finalBalance := s.getAccountBalance(accountAddr)
	require.True(finalBalance.LT(initialBalance.Sub(sendAmount.Amount)),
		"account balance should decrease by at least send amount: initial=%s, final=%s, sendAmount=%s",
		initialBalance, finalBalance, sendAmount.Amount)
}

// TestFeeAddressQuery verifies the FeeAddress query returns the correct address.
func (s *IntegrationTestSuite) TestFeeAddressQuery() {
	require := s.Require()

	queryClient := feeaddresstypes.NewQueryClient(s.cctx.GRPCClient)
	resp, err := queryClient.FeeAddress(s.cctx.GoContext(), &feeaddresstypes.QueryFeeAddressRequest{})
	require.NoError(err)
	require.Equal(feeaddresstypes.FeeAddressBech32, resp.FeeAddress)
}

// getAccountBalance queries the bank module for an account's utia balance.
func (s *IntegrationTestSuite) getAccountBalance(addr sdk.AccAddress) math.Int {
	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	resp, err := bqc.Balance(s.cctx.GoContext(), &banktypes.QueryBalanceRequest{
		Address: addr.String(),
		Denom:   appconsts.BondDenom,
	})
	s.Require().NoError(err)
	return resp.Balance.Amount
}

// getFeeCollectorBalance queries the bank module for the fee collector's utia balance.
func (s *IntegrationTestSuite) getFeeCollectorBalance() math.Int {
	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	feeCollectorAddr := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	resp, err := bqc.Balance(s.cctx.GoContext(), &banktypes.QueryBalanceRequest{
		Address: feeCollectorAddr.String(),
		Denom:   appconsts.BondDenom,
	})
	s.Require().NoError(err)
	return resp.Balance.Amount
}

// getFeeAddressBalance queries the bank module for the fee address's utia balance.
func (s *IntegrationTestSuite) getFeeAddressBalance() math.Int {
	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	resp, err := bqc.Balance(s.cctx.GoContext(), &banktypes.QueryBalanceRequest{
		Address: feeaddresstypes.FeeAddressBech32,
		Denom:   appconsts.BondDenom,
	})
	s.Require().NoError(err)
	return resp.Balance.Amount
}
