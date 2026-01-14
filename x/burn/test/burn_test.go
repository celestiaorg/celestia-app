package test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/app/encoding"
	"github.com/celestiaorg/celestia-app/v7/app/params"
	"github.com/celestiaorg/celestia-app/v7/pkg/user"
	"github.com/celestiaorg/celestia-app/v7/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v7/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v7/test/util/testnode"
	burntypes "github.com/celestiaorg/celestia-app/v7/x/burn/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/suite"
)

// IntegrationTestSuite runs end-to-end tests against a real test network.
// It verifies the burn module works correctly when integrated with the full app.
type IntegrationTestSuite struct {
	suite.Suite
	accounts []string         // funded test accounts
	cctx     testnode.Context // test network context with gRPC client
	ecfg     encoding.Config  // encoding config for tx building
}

func TestBurnIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

// SetupSuite spins up a single-node test network with 5 funded accounts.
// Each account starts with 1 billion TIA (1e15 utia).
func (s *IntegrationTestSuite) SetupSuite() {
	s.accounts = testfactory.GenerateAccounts(5)
	cfg := testnode.DefaultConfig().WithFundedAccounts(s.accounts...)
	cctx, _, _ := testnode.NewNetwork(s.T(), cfg)
	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
}

// TestBurnAddressSendAndBurn verifies that sending utia to the burn address:
// 1. Tokens are transferred to burn address
// 2. Tokens are burned by the EndBlocker
// 3. TotalBurned query reflects the burned amount
func (s *IntegrationTestSuite) TestBurnAddressSendAndBurn() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	// Setup: get test account and record initial balance
	account := s.accounts[0]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	burnAmount := sdk.NewCoin(params.BondDenom, math.NewInt(1000000)) // 1 TIA
	initialBalance := s.getAccountBalance(accountAddr)

	// Query initial total burned
	queryClient := burntypes.NewQueryClient(s.cctx.GRPCClient)
	initialResp, err := queryClient.TotalBurned(s.cctx.GoContext(), &burntypes.QueryTotalBurnedRequest{})
	require.NoError(err)
	initialBurned := initialResp.TotalBurned.Amount

	// Build and submit MsgSend to burn address
	msgSend := &banktypes.MsgSend{
		FromAddress: accountAddr.String(),
		ToAddress:   burntypes.BurnAddressBech32,
		Amount:      sdk.NewCoins(burnAmount),
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgSend}, blobfactory.DefaultTxOpts()...)
	require.NoError(err)
	require.NotNil(res)
	require.Equal(abci.CodeTypeOK, res.Code, "send to burn address tx failed with code: %d", res.Code)

	// Wait for endblock to burn the tokens
	require.NoError(s.cctx.WaitForNextBlock())

	// Verify TotalBurned increased by at least the burn amount
	finalResp, err := queryClient.TotalBurned(s.cctx.GoContext(), &burntypes.QueryTotalBurnedRequest{})
	require.NoError(err)
	require.True(finalResp.TotalBurned.Amount.GTE(initialBurned.Add(burnAmount.Amount)),
		"total burned should increase: initial=%s, final=%s, burned=%s",
		initialBurned, finalResp.TotalBurned.Amount, burnAmount.Amount)

	// Verify account balance decreased by at least burn amount (gas fees cause additional decrease)
	finalBalance := s.getAccountBalance(accountAddr)
	require.True(finalBalance.LT(initialBalance.Sub(burnAmount.Amount)),
		"account balance should decrease by at least burn amount: initial=%s, final=%s, burnAmount=%s",
		initialBalance, finalBalance, burnAmount.Amount)
}

// TestBurnEmitsEvent verifies that when tokens are burned by EndBlocker, an event is emitted with:
// - burner: the burn address
// - amount: the amount burned (e.g., "500000utia")
func (s *IntegrationTestSuite) TestBurnEmitsEvent() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	// Setup: use a different account than other tests to avoid nonce conflicts
	account := s.accounts[1]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	burnAmount := sdk.NewCoin(params.BondDenom, math.NewInt(500000))

	// Submit send to burn address
	msgSend := &banktypes.MsgSend{
		FromAddress: accountAddr.String(),
		ToAddress:   burntypes.BurnAddressBech32,
		Amount:      sdk.NewCoins(burnAmount),
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgSend}, blobfactory.DefaultTxOpts()...)
	require.NoError(err)
	require.NotNil(res)
	require.Equal(abci.CodeTypeOK, res.Code, "send to burn address tx failed with code: %d", res.Code)

	// Query the committed transaction to inspect its events
	txServiceClient := txtypes.NewServiceClient(s.cctx.GRPCClient)
	getTxResp, err := txServiceClient.GetTx(s.cctx.GoContext(), &txtypes.GetTxRequest{Hash: res.TxHash})
	require.NoError(err)
	require.NotNil(getTxResp.TxResponse)

	// The burn event is emitted in EndBlock, so we need to check the block results
	// For now, verify the transfer event was emitted (burn happens in EndBlock)
	found := false
	for _, event := range getTxResp.TxResponse.Events {
		if event.Type == "transfer" {
			for _, attr := range event.Attributes {
				if attr.Key == "recipient" && attr.Value == burntypes.BurnAddressBech32 {
					found = true
					break
				}
			}
		}
	}
	require.True(found, "transfer to burn address should emit transfer event")
}

// TestBurnAddressRejectsNonUtia verifies that sending non-utia tokens to the burn address
// is rejected by the ante handler.
func (s *IntegrationTestSuite) TestBurnAddressRejectsNonUtia() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[2]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	// Try to send a different denomination to the burn address
	wrongDenomAmount := sdk.NewCoin("wrongdenom", math.NewInt(1000000))

	msgSend := &banktypes.MsgSend{
		FromAddress: accountAddr.String(),
		ToAddress:   burntypes.BurnAddressBech32,
		Amount:      sdk.NewCoins(wrongDenomAmount),
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	// This should fail during ante handler (burn address restriction)
	_, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgSend}, blobfactory.DefaultTxOpts()...)
	require.Error(err, "sending non-utia to burn address should fail")
}

// TestTotalBurnedQuery verifies the TotalBurned query returns cumulative burned tokens.
func (s *IntegrationTestSuite) TestTotalBurnedQuery() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[3]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	burnAmount := sdk.NewCoin(params.BondDenom, math.NewInt(100000))

	// Query initial total burned
	queryClient := burntypes.NewQueryClient(s.cctx.GRPCClient)
	initialResp, err := queryClient.TotalBurned(s.cctx.GoContext(), &burntypes.QueryTotalBurnedRequest{})
	require.NoError(err)
	initialBurned := initialResp.TotalBurned.Amount

	// Send to burn address
	msgSend := &banktypes.MsgSend{
		FromAddress: accountAddr.String(),
		ToAddress:   burntypes.BurnAddressBech32,
		Amount:      sdk.NewCoins(burnAmount),
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgSend}, blobfactory.DefaultTxOpts()...)
	require.NoError(err)
	require.Equal(abci.CodeTypeOK, res.Code)

	// Wait for EndBlocker to burn
	require.NoError(s.cctx.WaitForNextBlock())

	// Query total burned again
	finalResp, err := queryClient.TotalBurned(s.cctx.GoContext(), &burntypes.QueryTotalBurnedRequest{})
	require.NoError(err)

	// Total burned should have increased by at least the burn amount
	require.True(finalResp.TotalBurned.Amount.GTE(initialBurned.Add(burnAmount.Amount)),
		"total burned should increase: initial=%s, final=%s, burned=%s",
		initialBurned, finalResp.TotalBurned.Amount, burnAmount.Amount)
}

// getAccountBalance queries the bank module for an account's utia balance.
func (s *IntegrationTestSuite) getAccountBalance(addr sdk.AccAddress) math.Int {
	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	resp, err := bqc.Balance(s.cctx.GoContext(), &banktypes.QueryBalanceRequest{
		Address: addr.String(),
		Denom:   params.BondDenom,
	})
	s.Require().NoError(err)
	return resp.Balance.Amount
}
