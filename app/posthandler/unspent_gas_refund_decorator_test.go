package posthandler_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestUnspentGasRefundDecorator(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping unspent gas refund decorator test in short mode.")
	}
	suite.Run(t, new(UnspentGasRefundDecoratorSuite))
}

type UnspentGasRefundDecoratorSuite struct {
	suite.Suite

	ctx    testnode.Context
	encCfg encoding.Config
	signer *user.Signer
}

func (s *UnspentGasRefundDecoratorSuite) SetupSuite() {
	s.encCfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.ctx, _, _ = testnode.NewNetwork(s.T(), testnode.DefaultConfig().WithFundedAccounts("a"))
	_, err := s.ctx.WaitForHeight(1)
	s.Require().NoError(err)
	rec, err := s.ctx.Keyring.Key("a")
	s.Require().NoError(err)
	addr, err := rec.GetAddress()
	s.Require().NoError(err)
	s.signer, err = user.SetupSigner(s.ctx.GoContext(), s.ctx.Keyring, s.ctx.GRPCClient, addr, s.encCfg)
	s.Require().NoError(err)
}

// TestGasConsumption verifies that the amount deducted from a user's balance is
// based on the gas consumed by the tx instead of the fee specified by the tx.
func (s *UnspentGasRefundDecoratorSuite) TestGasConsumption() {
	t := s.T()

	utiaToSend := int64(1)
	msg := bank.NewMsgSend(s.signer.Address(), testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, utiaToSend)))

	gasPrice := int64(1)
	gasLimit := uint64(1e6)
	fee := uint64(1e6) // 1 TIA
	// Note: gas price * gas limit = fee amount. So by setting gasLimit and fee
	// to the same value, these options set a gas price of 1utia.
	options := []user.TxOption{user.SetGasLimit(gasLimit), user.SetFee(fee)}

	balanceBefore := s.queryCurrentBalance(t)
	resp, err := s.signer.SubmitTx(s.ctx.GoContext(), []sdk.Msg{msg}, options...)
	require.NoError(t, err)

	require.EqualValues(t, abci.CodeTypeOK, resp.Code)
	balanceAfter := s.queryCurrentBalance(t)

	// Verify that the amount deducted does not depend on the fee set in the tx.
	amountDeducted := balanceBefore - balanceAfter - utiaToSend
	assert.NotEqual(t, int64(fee), amountDeducted)

	// Verify that the amount deducted depends on the actual gas consumed.
	gasConsumedBasedDeduction := resp.GasUsed * gasPrice
	assert.Equal(t, gasConsumedBasedDeduction, amountDeducted)

	// The gas consumed based deduction should be less than the fee because
	// the fee is 1 TIA.
	assert.Less(t, gasConsumedBasedDeduction, int64(fee))
}

func (s *UnspentGasRefundDecoratorSuite) queryCurrentBalance(t *testing.T) int64 {
	balanceQuery := bank.NewQueryClient(s.ctx.GRPCClient)
	balanceResp, err := balanceQuery.AllBalances(s.ctx.GoContext(), &bank.QueryAllBalancesRequest{Address: s.signer.Address().String()})
	require.NoError(t, err)
	return balanceResp.Balances.AmountOf(app.BondDenom).Int64()
}
