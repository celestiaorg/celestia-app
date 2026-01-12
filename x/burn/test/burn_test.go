package test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/app/params"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	burntypes "github.com/celestiaorg/celestia-app/v6/x/burn/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc/metadata"
)

const (
	utiaPerTIA = 1_000_000     // 1 TIA = 1,000,000 utia
	billion    = 1_000_000_000 // for readability in large amounts
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

// TestBurnDecreasesTotalSupply verifies that burning tokens:
// 1. Reduces the total token supply on-chain
// 2. Decreases the burner's account balance
func (s *IntegrationTestSuite) TestBurnDecreasesTotalSupply() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	// Setup: get test account and record initial balance
	account := s.accounts[0]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	burnAmount := sdk.NewCoin(params.BondDenom, math.NewInt(1000000)) // 1 TIA
	initialBalance := s.getAccountBalance(accountAddr)

	// Build and submit MsgBurn transaction
	msgBurn := &burntypes.MsgBurn{
		Signer: accountAddr.String(),
		Amount: burnAmount,
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgBurn}, blobfactory.DefaultTxOpts()...)
	require.NoError(err)
	require.NotNil(res)
	require.Equal(abci.CodeTypeOK, res.Code, "burn tx failed with code: %d", res.Code)

	// Query total supply at the burn block and the block before it.
	// We compare these two heights because inflation adds tokens each block,
	// so we need to isolate the burn's effect on supply.
	supplyAtBurnHeight := s.getTotalSupplyAtHeight(res.Height)
	supplyBeforeBurn := s.getTotalSupplyAtHeight(res.Height - 1)

	// Verify supply reflects burn: after burning, supply should be less than
	// (previous + burnAmount) since that's what supply would be without burning
	// (assuming inflation is less than burnAmount).
	require.True(supplyAtBurnHeight.AmountOf(params.BondDenom).LT(supplyBeforeBurn.AmountOf(params.BondDenom).Add(burnAmount.Amount)),
		"total supply should reflect burn: before=%s, after=%s, burnAmount=%s",
		supplyBeforeBurn.AmountOf(params.BondDenom), supplyAtBurnHeight.AmountOf(params.BondDenom), burnAmount.Amount)

	// Verify account balance decreased by at least burn amount (gas fees cause additional decrease)
	finalBalance := s.getAccountBalance(accountAddr)
	require.True(finalBalance.LT(initialBalance.Sub(burnAmount.Amount)),
		"account balance should decrease by at least burn amount: initial=%s, final=%s, burnAmount=%s",
		initialBalance, finalBalance, burnAmount.Amount)
}

// TestBurnEmitsEvent verifies that a successful burn emits an event with:
// - signer: the address that burned tokens
// - amount: the amount burned (e.g., "500000utia")
func (s *IntegrationTestSuite) TestBurnEmitsEvent() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	// Setup: use a different account than other tests to avoid nonce conflicts
	account := s.accounts[1]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	burnAmount := sdk.NewCoin(params.BondDenom, math.NewInt(500000))

	// Submit burn transaction
	msgBurn := &burntypes.MsgBurn{
		Signer: accountAddr.String(),
		Amount: burnAmount,
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgBurn}, blobfactory.DefaultTxOpts()...)
	require.NoError(err)
	require.NotNil(res)
	require.Equal(abci.CodeTypeOK, res.Code, "burn tx failed with code: %d", res.Code)

	// Query the committed transaction to inspect its events
	txServiceClient := txtypes.NewServiceClient(s.cctx.GRPCClient)
	getTxResp, err := txServiceClient.GetTx(s.cctx.GoContext(), &txtypes.GetTxRequest{Hash: res.TxHash})
	require.NoError(err)
	require.NotNil(getTxResp.TxResponse)

	// Find our burn event (filter by expected signer to avoid bank module's internal events)
	burnEvent, err := getBurnEvent(getTxResp.TxResponse.Events, accountAddr.String())
	require.NoError(err, "burn event should be emitted")
	require.Equal(accountAddr.String(), burnEvent.Signer)
	require.Equal(burnAmount.String(), burnEvent.Amount)
}

// TestBurnInsufficientBalance verifies that attempting to burn more tokens
// than the account holds results in an error (not a partial burn).
func (s *IntegrationTestSuite) TestBurnInsufficientBalance() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[2]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	// Try to burn 10 billion TIA - way more than the 1 billion TIA funded
	hugeAmount := sdk.NewCoin(params.BondDenom, math.NewInt(10*billion*utiaPerTIA))

	msgBurn := &burntypes.MsgBurn{
		Signer: accountAddr.String(),
		Amount: hugeAmount,
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	// This should fail during execution (insufficient funds error from bank module)
	_, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgBurn}, blobfactory.DefaultTxOpts()...)
	require.Error(err, "burn with insufficient balance should fail")
}

// TestBurnWrongDenom verifies that only utia (the bond denom) can be burned.
// Attempting to burn other denominations should fail ValidateBasic.
func (s *IntegrationTestSuite) TestBurnWrongDenom() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[3]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	// "wrongdenom" is not utia, so this should fail validation
	wrongDenomAmount := sdk.NewCoin("wrongdenom", math.NewInt(1000000))

	msgBurn := &burntypes.MsgBurn{
		Signer: accountAddr.String(),
		Amount: wrongDenomAmount,
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	// This should fail during ValidateBasic (wrong denom)
	_, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgBurn}, blobfactory.DefaultTxOpts()...)
	require.Error(err, "burn with wrong denom should fail")
}

// getTotalSupplyAtHeight queries the bank module for total token supply at a specific block height.
// This allows comparing supply before and after a burn to isolate its effect from inflation.
func (s *IntegrationTestSuite) getTotalSupplyAtHeight(height int64) sdk.Coins {
	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	// Set gRPC metadata to query at a specific historical height
	ctx := metadata.AppendToOutgoingContext(context.Background(), grpctypes.GRPCBlockHeightHeader, fmt.Sprintf("%d", height))
	resp, err := bqc.TotalSupply(ctx, &banktypes.QueryTotalSupplyRequest{})
	s.Require().NoError(err)
	return resp.Supply
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

// BurnEvent represents the parsed attributes from a burn event.
type BurnEvent struct {
	Signer string // bech32 address of the account that burned tokens
	Amount string // amount burned, e.g., "1000000utia"
}

// getBurnEvent searches transaction events for our burn module's typed EventBurn.
// It filters by expectedSigner because the bank module also emits burn-related events
// with different addresses (e.g., the module account).
func getBurnEvent(events []abci.Event, expectedSigner string) (BurnEvent, error) {
	// Typed events use the proto message name as the event type.
	// We use the literal string here because proto.MessageName() returns empty
	// when called at package init time (before proto types are registered).
	const eventType = "celestia.burn.v1.EventBurn"
	for _, event := range events {
		if event.Type != eventType {
			continue
		}
		var signer, amount string
		for _, attr := range event.Attributes {
			// Typed event values are JSON-encoded, so strings are quoted.
			// We trim the surrounding quotes to get the raw value.
			value := strings.Trim(attr.Value, "\"")
			switch attr.Key {
			case "signer":
				signer = value
			case "amount":
				amount = value
			}
		}
		// Only return if this matches our expected signer (filters out bank module events)
		if signer == expectedSigner {
			return BurnEvent{Signer: signer, Amount: amount}, nil
		}
	}
	return BurnEvent{}, fmt.Errorf("burn event with signer %s not found", expectedSigner)
}
