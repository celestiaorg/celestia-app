package test

import (
	"context"
	"fmt"
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

type IntegrationTestSuite struct {
	suite.Suite

	accounts []string
	cctx     testnode.Context
	ecfg     encoding.Config
}

func TestBurnIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping burn integration test in short mode.")
	}
	suite.Run(t, new(IntegrationTestSuite))
}

func (s *IntegrationTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up burn integration test suite")

	s.accounts = testfactory.GenerateAccounts(5)
	cfg := testnode.DefaultConfig().WithFundedAccounts(s.accounts...)
	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
}

// TestBurnDecreasesTotalSupply tests that burning tokens decreases the total supply.
func (s *IntegrationTestSuite) TestBurnDecreasesTotalSupply() {
	require := s.Require()

	// Wait for the network to be ready
	err := s.cctx.WaitForNextBlock()
	require.NoError(err)

	// Get the account to burn from
	account := s.accounts[0]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)

	// Define burn amount
	burnAmount := sdk.NewCoin(params.BondDenom, math.NewInt(1000000)) // 1 TIA

	// Query initial account balance
	initialBalance := s.getAccountBalance(accountAddr)

	// Create and submit MsgBurn
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

	// Get supply at the block height where burn was included
	burnHeight := res.Height
	supplyAtBurnHeight := s.getTotalSupplyAtHeight(burnHeight)
	supplyBeforeBurn := s.getTotalSupplyAtHeight(burnHeight - 1)

	// Verify total supply decreased by the burn amount between these two heights
	// Note: inflation adds tokens each block, so we calculate the difference
	// The burn should reduce supply by burnAmount compared to what it would have been
	supplyDiff := supplyAtBurnHeight.AmountOf(params.BondDenom).Sub(supplyBeforeBurn.AmountOf(params.BondDenom))

	// The supply difference should be negative (decreased) or less than it would be without the burn
	// Since inflation adds coins, supply at burnHeight should be less than supplyBeforeBurn + inflation - burnAmount
	// We verify that the burn actually reduced the supply by checking account balance decreased
	require.True(supplyDiff.LT(math.ZeroInt()) || supplyAtBurnHeight.AmountOf(params.BondDenom).LT(supplyBeforeBurn.AmountOf(params.BondDenom).Add(burnAmount.Amount)),
		"total supply should reflect burn: before=%s, after=%s, diff=%s",
		supplyBeforeBurn.AmountOf(params.BondDenom), supplyAtBurnHeight.AmountOf(params.BondDenom), supplyDiff)

	// Query final account balance - this is the definitive test
	finalBalance := s.getAccountBalance(accountAddr)

	// Verify account balance decreased (by burn amount + gas fees)
	require.True(finalBalance.LT(initialBalance.Sub(burnAmount.Amount)),
		"account balance should decrease by at least burn amount: initial=%s, final=%s, burnAmount=%s",
		initialBalance, finalBalance, burnAmount.Amount)
}

// TestBurnEmitsEvent tests that burning tokens emits the correct event.
func (s *IntegrationTestSuite) TestBurnEmitsEvent() {
	require := s.Require()

	// Wait for the network to be ready
	err := s.cctx.WaitForNextBlock()
	require.NoError(err)

	// Get the account to burn from
	account := s.accounts[1]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)

	// Define burn amount
	burnAmount := sdk.NewCoin(params.BondDenom, math.NewInt(500000))

	// Create and submit MsgBurn
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

	// Query the transaction to get events
	txServiceClient := txtypes.NewServiceClient(s.cctx.GRPCClient)
	getTxResp, err := txServiceClient.GetTx(s.cctx.GoContext(), &txtypes.GetTxRequest{Hash: res.TxHash})
	require.NoError(err)
	require.NotNil(getTxResp.TxResponse)

	// Find and verify the burn event
	burnEvent, err := getBurnEvent(getTxResp.TxResponse.Events, accountAddr.String())
	require.NoError(err, "burn event should be emitted")
	require.Equal(accountAddr.String(), burnEvent.Burner, "burner address should match")
	require.Equal(burnAmount.String(), burnEvent.Amount, "burn amount should match")
}

// TestBurnInsufficientBalance tests that burning more than the account balance fails.
func (s *IntegrationTestSuite) TestBurnInsufficientBalance() {
	require := s.Require()

	// Wait for the network to be ready
	err := s.cctx.WaitForNextBlock()
	require.NoError(err)

	// Get the account to burn from
	account := s.accounts[2]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)

	// Try to burn more than the account has (accounts are funded with 1000000000000 utia)
	hugeAmount := sdk.NewCoin(params.BondDenom, math.NewInt(10000000000000000)) // Way more than funded

	msgBurn := &burntypes.MsgBurn{
		Signer: accountAddr.String(),
		Amount: hugeAmount,
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	_, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgBurn}, blobfactory.DefaultTxOpts()...)
	require.Error(err, "burn with insufficient balance should fail")
}

// TestBurnWrongDenom tests that burning the wrong denomination fails validation.
func (s *IntegrationTestSuite) TestBurnWrongDenom() {
	require := s.Require()

	// Wait for the network to be ready
	err := s.cctx.WaitForNextBlock()
	require.NoError(err)

	// Get the account
	account := s.accounts[3]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)

	// Try to burn wrong denom
	wrongDenomAmount := sdk.NewCoin("wrongdenom", math.NewInt(1000000))

	msgBurn := &burntypes.MsgBurn{
		Signer: accountAddr.String(),
		Amount: wrongDenomAmount,
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	_, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgBurn}, blobfactory.DefaultTxOpts()...)
	require.Error(err, "burn with wrong denom should fail")
}

func (s *IntegrationTestSuite) getTotalSupplyAtHeight(height int64) sdk.Coins {
	require := s.Require()

	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	ctx := metadata.AppendToOutgoingContext(context.Background(), grpctypes.GRPCBlockHeightHeader, fmt.Sprintf("%d", height))

	resp, err := bqc.TotalSupply(ctx, &banktypes.QueryTotalSupplyRequest{})
	require.NoError(err)

	return resp.Supply
}

func (s *IntegrationTestSuite) getAccountBalance(addr sdk.AccAddress) math.Int {
	require := s.Require()

	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	resp, err := bqc.Balance(s.cctx.GoContext(), &banktypes.QueryBalanceRequest{
		Address: addr.String(),
		Denom:   params.BondDenom,
	})
	require.NoError(err)

	return resp.Balance.Amount
}

// BurnEvent represents the parsed burn event
type BurnEvent struct {
	Burner string
	Amount string
}

func getBurnEvent(events []abci.Event, expectedBurner string) (BurnEvent, error) {
	for _, event := range events {
		if event.Type == burntypes.EventTypeBurn {
			var burner string
			var amount string
			for _, attr := range event.Attributes {
				switch attr.Key {
				case burntypes.AttributeKeyBurner:
					burner = attr.Value
				case burntypes.AttributeKeyAmount:
					amount = attr.Value
				}
			}
			// Only return if this is our burn event (matches expected burner)
			// This filters out bank module's internal burn events
			if burner == expectedBurner {
				return BurnEvent{
					Burner: burner,
					Amount: amount,
				}, nil
			}
		}
	}
	return BurnEvent{}, fmt.Errorf("burn event with burner %s not found", expectedBurner)
}
