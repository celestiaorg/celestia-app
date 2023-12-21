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

const (
	utia = 1
	tia  = 1e6
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

	type testCase struct {
		name     string
		gasLimit uint64
		fee      uint64
		want     int64
	}

	testCases := []testCase{
		{
			name: "at most half of the fee should be refunded",
			// Note: gasPrice * gasLimit = fee. So by setting gasLimit and fee to the
			// same value, these options set a gasPrice of 1utia.
			gasLimit: 1e6,
			fee:      1 * tia,
			want:     1 * tia * .5,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			options := []user.TxOption{user.SetGasLimit(tc.gasLimit), user.SetFee(tc.fee)}
			msg := upgradetypes.NewMsgTryUpgrade(s.signer.Address())

			resp, err := s.signer.SubmitTx(s.ctx.GoContext(), []sdk.Msg{msg}, options...)
			require.NoError(t, err)
			require.EqualValues(t, abci.CodeTypeOK, resp.Code)

			got := calculateNetFee(t, resp, s.signer.Address().String())
			assert.Equal(t, tc.want, got)
		})
	}
}

// calculateNetFee calculates the fee that signer paid for the tx based on
// events in the TxResponse.
func calculateNetFee(t *testing.T, resp *sdk.TxResponse, signer string) (netFee int64) {
	assert.NotNil(t, resp)
	transfers := getTransfers(t, resp.Events)
	for _, transfer := range transfers {
		if transfer.from == signer {
			netFee += transfer.amount
		}
		if transfer.recipient == signer {
			netFee -= transfer.amount
		}
	}
	return netFee
}

func getTransfers(t *testing.T, events []abci.Event) (transfers []transferEvent) {
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

// transferEvent is a struct based on the transfer event type emitted by the
// bank module.
type transferEvent struct {
	recipient string
	from      string
	amount    int64
}
