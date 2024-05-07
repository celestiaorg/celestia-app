package user_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/rand"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

func TestTxClientTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}
	suite.Run(t, new(TxClientTestSuite))
}

type TxClientTestSuite struct {
	suite.Suite

	ctx    testnode.Context
	encCfg encoding.Config
	txClient *user.TxClient
}

func (s *TxClientTestSuite) SetupSuite() {
	s.encCfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.ctx, _, _ = testnode.NewNetwork(s.T(), testnode.DefaultConfig().WithFundedAccounts("a"))
	_, err := s.ctx.WaitForHeight(1)
	s.Require().NoError(err)
	s.txClient, err = user.SetupTxClient(s.ctx.GoContext(), s.ctx.Keyring, s.ctx.GRPCClient, s.encCfg, user.WithGasMultiplier(1.2))
	s.Require().NoError(err)
}

func (s *TxClientTestSuite) TestSubmitPayForBlob() {
	t := s.T()
	blobs := blobfactory.ManyRandBlobs(rand.NewRand(), 1e3, 1e4)
	fee := user.SetFee(1e6)
	gas := user.SetGasLimit(1e6)
	subCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := s.txClient.SubmitPayForBlob(subCtx, blobs, fee, gas)
	require.NoError(t, err)
	require.EqualValues(t, 0, resp.Code)
}

func (s *TxClientTestSuite) TestSubmitTx() {
	t := s.T()
	fee := user.SetFee(1e6)
	gas := user.SetGasLimit(1e6)
	addr := s.txClient.DefaultAddress()
	msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 10)))
	resp, err := s.txClient.SubmitTx(s.ctx.GoContext(), []sdk.Msg{msg}, fee, gas)
	require.NoError(t, err)
	require.EqualValues(t, 0, resp.Code)
}

func (s *TxClientTestSuite) TestConfirmTx() {
	t := s.T()

	fee := user.SetFee(1e6)
	gas := user.SetGasLimit(1e6)

	t.Run("deadline exceeded when the context times out", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(s.ctx.GoContext(), time.Second)
		defer cancel()
		_, err := s.txClient.ConfirmTx(ctx, "E32BD15CAF57AF15D17B0D63CF4E63A9835DD1CEBB059C335C79586BC3013728")
		require.Error(t, err)
		require.Contains(t, err.Error(), context.DeadlineExceeded.Error())
	})

	t.Run("should error when tx is not found", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(s.ctx.GoContext(), 5*time.Second)
		defer cancel()
		_, err := s.txClient.ConfirmTx(ctx, "not found tx")
		require.Error(t, err)
	})

	t.Run("should success when tx is found immediately", func(t *testing.T) {
		addr := s.txClient.DefaultAddress()
		msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 10)))
		resp, err := s.txClient.BroadcastTx(s.ctx.GoContext(), []sdk.Msg{msg}, fee, gas)
		require.NoError(t, err)
		require.NotNil(t, resp)
		ctx, cancel := context.WithTimeout(s.ctx.GoContext(), 30*time.Second)
		defer cancel()
		resp, err = s.txClient.ConfirmTx(ctx, resp.TxHash)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)
	})

	t.Run("should error when tx is found with a non-zero error code", func(t *testing.T) {
		balance := s.queryCurrentBalance(t)
		addr := s.txClient.DefaultAddress()
		// Create a msg send with out of balance, ensure this tx fails
		msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 1+balance)))
		resp, err := s.txClient.BroadcastTx(s.ctx.GoContext(), []sdk.Msg{msg}, fee, gas)
		require.NoError(t, err)
		require.NotNil(t, resp)
		resp, err = s.txClient.ConfirmTx(s.ctx.GoContext(), resp.TxHash)
		require.Error(t, err)
		require.NotEqual(t, abci.CodeTypeOK, resp.Code)
	})
}

func (s *TxClientTestSuite) TestGasEstimation() {
	addr := s.txClient.DefaultAddress()
	msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 10)))
	gas, err := s.txClient.EstimateGas(s.ctx.GoContext(), []sdk.Msg{msg})
	require.NoError(s.T(), err)
	require.Greater(s.T(), gas, uint64(0))
}

// TestGasConsumption verifies that the amount deducted from a user's balance is
// based on the fee provided in the tx instead of the gas used by the tx. This
// behavior leads to poor UX because tx submitters must over-estimate the amount
// of gas that their tx will consume and they are not refunded for the excess.
func (s *TxClientTestSuite) TestGasConsumption() {
	t := s.T()

	utiaToSend := int64(1)
	addr := s.txClient.DefaultAddress()
	msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, utiaToSend)))

	gasPrice := int64(1)
	gasLimit := uint64(1e6)
	fee := uint64(1e6) // 1 TIA
	// Note: gas price * gas limit = fee amount. So by setting gasLimit and fee
	// to the same value, these options set a gas price of 1utia.
	options := []user.TxOption{user.SetGasLimit(gasLimit), user.SetFee(fee)}

	balanceBefore := s.queryCurrentBalance(t)
	resp, err := s.txClient.SubmitTx(s.ctx.GoContext(), []sdk.Msg{msg}, options...)
	require.NoError(t, err)

	require.EqualValues(t, abci.CodeTypeOK, resp.Code)
	balanceAfter := s.queryCurrentBalance(t)

	// verify that the amount deducted depends on the fee set in the tx.
	amountDeducted := balanceBefore - balanceAfter - utiaToSend
	require.Equal(t, int64(fee), amountDeducted)

	// verify that the amount deducted does not depend on the actual gas used.
	gasUsedBasedDeduction := resp.GasUsed * gasPrice
	require.NotEqual(t, gasUsedBasedDeduction, amountDeducted)
	// The gas used based deduction should be less than the fee because the fee is 1 TIA.
	require.Less(t, gasUsedBasedDeduction, int64(fee))
}

func (s *TxClientTestSuite) queryCurrentBalance(t *testing.T) int64 {
	balanceQuery := bank.NewQueryClient(s.ctx.GRPCClient)
	addr := s.txClient.DefaultAddress()
	balanceResp, err := balanceQuery.AllBalances(s.ctx.GoContext(), &bank.QueryAllBalancesRequest{Address: addr.String()})
	require.NoError(t, err)
	return balanceResp.Balances.AmountOf(app.BondDenom).Int64()
}
