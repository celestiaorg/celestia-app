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
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/suite"
)

const utiaPerTIA = 1_000_000 // 1 TIA = 1,000,000 utia

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
	s.accounts = testfactory.GenerateAccounts(2)
	cfg := testnode.DefaultConfig().WithFundedAccounts(s.accounts...)
	cctx, _, _ := testnode.NewNetwork(s.T(), cfg)
	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
}

// TestFeeAddressSendAndForward verifies the E2E flow: send utia to fee address,
// PrepareProposal injects MsgForwardFees, and tokens are forwarded to fee collector.
func (s *IntegrationTestSuite) TestFeeAddressSendAndForward() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	// Step 1: Verify fee address starts with zero balance
	initialFeeAddressBalance := s.getFeeAddressBalance()
	require.True(initialFeeAddressBalance.IsZero(),
		"fee address should start with zero balance: balance=%s", initialFeeAddressBalance)

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

	// Wait for next block to ensure MsgForwardFees has forwarded the tokens
	require.NoError(s.cctx.WaitForNextBlock())

	// Step 2: Verify fee address is empty (MsgForwardFees forwarded tokens to fee collector)
	finalFeeAddressBalance := s.getFeeAddressBalance()
	require.True(finalFeeAddressBalance.IsZero(),
		"fee address should be empty after MsgForwardFees forwards tokens: balance=%s",
		finalFeeAddressBalance)

	// Verify account balance decreased by at least send amount (gas fees cause additional decrease)
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

// TestUserSubmittedMsgForwardFeesRejected verifies users cannot submit MsgForwardFees
// directly - it must be protocol-injected only (CIP-43).
func (s *IntegrationTestSuite) TestUserSubmittedMsgForwardFeesRejected() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[1]
	msgForwardFees := feeaddresstypes.NewMsgForwardFees()

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	_, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgForwardFees}, blobfactory.DefaultTxOpts()...)
	require.Error(err, "user-submitted MsgForwardFees should be rejected")
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

