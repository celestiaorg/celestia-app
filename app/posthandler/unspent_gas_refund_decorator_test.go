package posthandler_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	upgradetypes "github.com/celestiaorg/celestia-app/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
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
// not based on the fee specified by the tx.
func (s *UnspentGasRefundDecoratorSuite) TestGasConsumption() {
	t := s.T()

	gasLimit := uint64(1e6)
	fee := uint64(1e6) // 1 TIA
	// Note: gasPrice * gasLimit = fee. So by setting gasLimit and fee to the
	// same value, these options set a gasPrice of 1utia.
	options := []user.TxOption{user.SetGasLimit(gasLimit), user.SetFee(fee)}

	msg := upgradetypes.NewMsgTryUpgrade(s.signer.Address())
	resp, err := s.signer.SubmitTx(s.ctx.GoContext(), []sdk.Msg{msg}, options...)
	require.NoError(t, err)

	require.EqualValues(t, abci.CodeTypeOK, resp.Code)
	netFee := calculateNetFee(t, resp, s.signer.Address().String())

	assert.NotEqual(t, int64(fee), netFee)

	want := int64(fee) / 2
	assert.Equal(t, want, netFee)
}

func calculateNetFee(t *testing.T, resp *sdk.TxResponse, signer string) (netFee int64) {
	if resp == nil {
		return 0
	}
	transfers := filterTransfers(t, resp.Events)
	for _, transfer := range transfers {
		if transfer.from == signer {
			// deduct fee decorator
			netFee += transfer.amount
		}
		if transfer.recipient == signer {
			// unspent gas refund decorator
			netFee -= transfer.amount
		}
	}
	return netFee
}

type transferEvent struct {
	recipient string
	from      string
	amount    int64
}

func filterTransfers(t *testing.T, events []abci.Event) (transfers []transferEvent) {
	for _, event := range events {
		if event.Type == banktypes.EventTypeTransfer {
			amount, err := strconv.ParseInt(strings.TrimSuffix(string(event.Attributes[2].Value), "utia"), 10, 64)
			assert.NoError(t, err)
			transfer := transferEvent{
				recipient: string(event.Attributes[0].Value),
				from:      string(event.Attributes[1].Value),
				amount:    amount,
			}
			transfers = append(transfers, transfer)
		}
	}
	return transfers
}

func (s *UnspentGasRefundDecoratorSuite) queryCurrentBalance(t *testing.T) int64 {
	balanceQuery := banktypes.NewQueryClient(s.ctx.GRPCClient)
	balanceResp, err := balanceQuery.AllBalances(s.ctx.GoContext(), &banktypes.QueryAllBalancesRequest{Address: s.signer.Address().String()})
	require.NoError(t, err)
	return balanceResp.Balances.AmountOf(app.BondDenom).Int64()
}
