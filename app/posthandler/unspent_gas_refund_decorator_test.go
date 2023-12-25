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
	"github.com/cosmos/cosmos-sdk/x/feegrant"
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

	ctx      testnode.Context
	encCfg   encoding.Config
	signer   *user.Signer
	feePayer *user.Signer
}

func (s *UnspentGasRefundDecoratorSuite) SetupSuite() {
	s.encCfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.ctx, _, _ = testnode.NewNetwork(s.T(), testnode.DefaultConfig().WithFundedAccounts("a", "b"))
	_, err := s.ctx.WaitForHeight(1)
	s.Require().NoError(err)

	recordA, err := s.ctx.Keyring.Key("a")
	s.Require().NoError(err)
	addrA, err := recordA.GetAddress()
	s.Require().NoError(err)
	s.signer, err = user.SetupSigner(s.ctx.GoContext(), s.ctx.Keyring, s.ctx.GRPCClient, addrA, s.encCfg)
	s.Require().NoError(err)

	recordB, err := s.ctx.Keyring.Key("b")
	s.Require().NoError(err)
	addrB, err := recordB.GetAddress()
	s.Require().NoError(err)
	s.feePayer, err = user.SetupSigner(s.ctx.GoContext(), s.ctx.Keyring, s.ctx.GRPCClient, addrB, s.encCfg)
	s.Require().NoError(err)

	msg, err := feegrant.NewMsgGrantAllowance(&feegrant.BasicAllowance{}, s.feePayer.Address(), s.signer.Address())
	s.Require().NoError(err)
	options := []user.TxOption{user.SetGasLimit(1e6), user.SetFee(tia)}
	s.feePayer.SubmitTx(s.ctx.GoContext(), []sdk.Msg{msg}, options...)
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
			name: "part of the fee should be refunded",
			// Note: gasPrice * gasLimit = fee. So by setting gasLimit and fee to the
			// same value, these options set a gasPrice of 1 utia.
			gasLimit: 1e5, // 100_000
			fee:      1e5, // 100_000 utia
			want:     61_931,
		},
		{
			name:     "at most half of the fee should be refunded",
			gasLimit: 1e6,
			fee:      tia,
			want:     tia * .5,
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

// TODO: consider collapsing this test with the test above.
func (s *UnspentGasRefundDecoratorSuite) TestRefundRecipient() {
	t := s.T()

	type testCase struct {
		name                string
		gasLimit            uint64
		fee                 uint64
		wantNetFee          int64
		feePayer            sdk.AccAddress
		wantRefundRecipient sdk.AccAddress
	}

	testCases := []testCase{
		{
			name:                "refund should be sent to signer if fee payer is unspecified",
			gasLimit:            1e6,
			fee:                 tia,
			wantNetFee:          tia * .5,
			wantRefundRecipient: s.signer.Address(),
		},
		{
			name:                "refund should be sent to fee payer if specified",
			gasLimit:            1e6,
			fee:                 tia,
			feePayer:            s.feePayer.Address(),
			wantNetFee:          tia * .5,
			wantRefundRecipient: s.feePayer.Address(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			options := []user.TxOption{user.SetGasLimit(tc.gasLimit), user.SetFee(tc.fee)}
			if tc.feePayer != nil {
				// Cosmos SDK has confusing naming but invoke SetFeeGranter
				// instead of SetFeePayer.
				//
				// https://github.com/cosmos/cosmos-sdk/issues/18886
				options = append(options, user.SetFeeGranter(tc.feePayer))
			}
			msg := upgradetypes.NewMsgTryUpgrade(s.signer.Address())

			resp, err := s.signer.SubmitTx(s.ctx.GoContext(), []sdk.Msg{msg}, options...)
			require.NoError(t, err)
			require.EqualValues(t, abci.CodeTypeOK, resp.Code)

			got := calculateNetFee(t, resp, tc.wantRefundRecipient.String())
			assert.Equal(t, tc.wantNetFee, got)
		})
	}
}

// calculateNetFee calculates the fee that feePayer paid for the tx based on
// events in the TxResponse.
func calculateNetFee(t *testing.T, resp *sdk.TxResponse, feePayer string) (netFee int64) {
	assert.NotNil(t, resp)
	transfers := getTransfers(t, resp.Events)
	for _, transfer := range transfers {
		if transfer.from == feePayer {
			netFee += transfer.amount
		}
		if transfer.recipient == feePayer {
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
