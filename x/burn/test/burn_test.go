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
	s.accounts = testfactory.GenerateAccounts(5)
	cfg := testnode.DefaultConfig().WithFundedAccounts(s.accounts...)
	cctx, _, _ := testnode.NewNetwork(s.T(), cfg)
	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
}

func (s *IntegrationTestSuite) TestBurnDecreasesTotalSupply() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[0]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	burnAmount := sdk.NewCoin(params.BondDenom, math.NewInt(1000000))
	initialBalance := s.getAccountBalance(accountAddr)

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

	supplyAtBurnHeight := s.getTotalSupplyAtHeight(res.Height)
	supplyBeforeBurn := s.getTotalSupplyAtHeight(res.Height - 1)

	// Verify total supply decreased (accounting for inflation that adds tokens each block)
	supplyDiff := supplyAtBurnHeight.AmountOf(params.BondDenom).Sub(supplyBeforeBurn.AmountOf(params.BondDenom))
	require.True(supplyDiff.LT(math.ZeroInt()) || supplyAtBurnHeight.AmountOf(params.BondDenom).LT(supplyBeforeBurn.AmountOf(params.BondDenom).Add(burnAmount.Amount)),
		"total supply should reflect burn: before=%s, after=%s, diff=%s",
		supplyBeforeBurn.AmountOf(params.BondDenom), supplyAtBurnHeight.AmountOf(params.BondDenom), supplyDiff)

	// Verify account balance decreased by at least burn amount (plus gas fees)
	finalBalance := s.getAccountBalance(accountAddr)
	require.True(finalBalance.LT(initialBalance.Sub(burnAmount.Amount)),
		"account balance should decrease by at least burn amount: initial=%s, final=%s, burnAmount=%s",
		initialBalance, finalBalance, burnAmount.Amount)
}

func (s *IntegrationTestSuite) TestBurnEmitsEvent() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[1]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	burnAmount := sdk.NewCoin(params.BondDenom, math.NewInt(500000))

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

	txServiceClient := txtypes.NewServiceClient(s.cctx.GRPCClient)
	getTxResp, err := txServiceClient.GetTx(s.cctx.GoContext(), &txtypes.GetTxRequest{Hash: res.TxHash})
	require.NoError(err)
	require.NotNil(getTxResp.TxResponse)

	burnEvent, err := getBurnEvent(getTxResp.TxResponse.Events, accountAddr.String())
	require.NoError(err, "burn event should be emitted")
	require.Equal(accountAddr.String(), burnEvent.Burner)
	require.Equal(burnAmount.String(), burnEvent.Amount)
}

func (s *IntegrationTestSuite) TestBurnInsufficientBalance() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[2]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
	hugeAmount := sdk.NewCoin(params.BondDenom, math.NewInt(10000000000000000))

	msgBurn := &burntypes.MsgBurn{
		Signer: accountAddr.String(),
		Amount: hugeAmount,
	}

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(account))
	require.NoError(err)

	_, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgBurn}, blobfactory.DefaultTxOpts()...)
	require.Error(err, "burn with insufficient balance should fail")
}

func (s *IntegrationTestSuite) TestBurnWrongDenom() {
	require := s.Require()
	require.NoError(s.cctx.WaitForNextBlock())

	account := s.accounts[3]
	accountAddr := testfactory.GetAddress(s.cctx.Keyring, account)
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
	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	ctx := metadata.AppendToOutgoingContext(context.Background(), grpctypes.GRPCBlockHeightHeader, fmt.Sprintf("%d", height))
	resp, err := bqc.TotalSupply(ctx, &banktypes.QueryTotalSupplyRequest{})
	s.Require().NoError(err)
	return resp.Supply
}

func (s *IntegrationTestSuite) getAccountBalance(addr sdk.AccAddress) math.Int {
	bqc := banktypes.NewQueryClient(s.cctx.GRPCClient)
	resp, err := bqc.Balance(s.cctx.GoContext(), &banktypes.QueryBalanceRequest{
		Address: addr.String(),
		Denom:   params.BondDenom,
	})
	s.Require().NoError(err)
	return resp.Balance.Amount
}

type BurnEvent struct {
	Burner string
	Amount string
}

func getBurnEvent(events []abci.Event, expectedBurner string) (BurnEvent, error) {
	for _, event := range events {
		if event.Type != burntypes.EventTypeBurn {
			continue
		}
		var burner, amount string
		for _, attr := range event.Attributes {
			switch attr.Key {
			case burntypes.AttributeKeyBurner:
				burner = attr.Value
			case burntypes.AttributeKeyAmount:
				amount = attr.Value
			}
		}
		if burner == expectedBurner {
			return BurnEvent{Burner: burner, Amount: amount}, nil
		}
	}
	return BurnEvent{}, fmt.Errorf("burn event with burner %s not found", expectedBurner)
}
