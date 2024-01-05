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
	"github.com/cosmos/cosmos-sdk/types/tx"
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

func TestRefundGasRemaining(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping refund gas remaining test in short mode.")
	}
	suite.Run(t, new(RefundGasRemainingSuite))
}

type RefundGasRemainingSuite struct {
	suite.Suite

	ctx        testnode.Context
	encCfg     encoding.Config
	signer     *user.Signer
	feeGranter *user.Signer
}

func (s *RefundGasRemainingSuite) SetupSuite() {
	require := s.Require()
	s.encCfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.ctx, _, _ = testnode.NewNetwork(s.T(), testnode.DefaultConfig().WithFundedAccounts("signer", "feegranter"))
	_, err := s.ctx.WaitForHeight(1)
	require.NoError(err)

	recordA, err := s.ctx.Keyring.Key("signer")
	require.NoError(err)
	addrA, err := recordA.GetAddress()
	require.NoError(err)
	s.signer, err = user.SetupSigner(s.ctx.GoContext(), s.ctx.Keyring, s.ctx.GRPCClient, addrA, s.encCfg)
	require.NoError(err)

	recordB, err := s.ctx.Keyring.Key("feegranter")
	require.NoError(err)
	addrB, err := recordB.GetAddress()
	require.NoError(err)
	s.feeGranter, err = user.SetupSigner(s.ctx.GoContext(), s.ctx.Keyring, s.ctx.GRPCClient, addrB, s.encCfg)
	require.NoError(err)

	msg, err := feegrant.NewMsgGrantAllowance(&feegrant.BasicAllowance{}, s.feeGranter.Address(), s.signer.Address())
	require.NoError(err)
	options := []user.TxOption{user.SetGasLimit(1e6), user.SetFee(tia)}
	resp, err := s.feeGranter.SubmitTx(s.ctx.GoContext(), []sdk.Msg{msg}, options...)
	require.NoError(err)
	require.Equal(abci.CodeTypeOK, resp.Code)
}

func (s *RefundGasRemainingSuite) TestDecorator() {
	t := s.T()

	type testCase struct {
		name                string
		gasLimit            uint64
		fee                 uint64
		feeGranter          sdk.AccAddress
		wantRefund          int64
		wantRefundRecipient sdk.AccAddress
	}

	testCases := []testCase{
		{
			// Note: gasPrice * gasLimit = fee. So gasPrice = 1 utia.
			name:                "part of the fee should be refunded",
			gasLimit:            100_000,
			fee:                 100_000 * utia,
			wantRefund:          23_069,
			wantRefundRecipient: s.signer.Address(),
		},
		{
			// Note: gasPrice * gasLimit = fee. So gasPrice = 10 utia.
			name:                "refund should vary based on gasPrice",
			gasLimit:            100_000,
			fee:                 tia, // 1_000_000 utia
			wantRefund:          229730,
			wantRefundRecipient: s.signer.Address(),
		},
		{
			// Note: gasPrice * gasLimit = fee. So gasPrice = 1 utia.
			name:                "refund should be at most half of the fee",
			gasLimit:            1_000_000, // 1_000_000 is way higher than gas consumed by this tx
			fee:                 tia,
			wantRefund:          38_513,
			wantRefundRecipient: s.signer.Address(),
		},
		{
			// Note: gasPrice * gasLimit = fee. So gasPrice = 1 utia.
			name:                "refund should be sent to fee granter if specified",
			gasLimit:            1_000_000,
			fee:                 tia,
			feeGranter:          s.feeGranter.Address(),
			wantRefund:          44075,
			wantRefundRecipient: s.feeGranter.Address(),
		},
		{
			name:                "no refund should be sent if gasLimit isn't high enough to pay for the refund gas cost",
			gasLimit:            65_000,
			fee:                 65_000,
			wantRefund:          0,
			wantRefundRecipient: s.signer.Address(),
		},
		{
			name:                "no refund should be sent if gasPrice is extremely low because the refund amount truncates to zero",
			gasLimit:            tx.MaxGasWanted,
			fee:                 utia,
			wantRefund:          0,
			wantRefundRecipient: s.signer.Address(),
		},
		{
			name: "should not cause an int overflow if gas limit = max gas wanted",
			// NOTE: these test cases do not need to consider gasLimit >
			// MaxGasWanted because that will result in an error on
			// tx.ValidateBasic().
			gasLimit:            tx.MaxGasWanted,
			fee:                 1_000_000_000 * tia,
			wantRefund:          4 * utia,
			wantRefundRecipient: s.signer.Address(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			options := []user.TxOption{user.SetGasLimit(tc.gasLimit), user.SetFee(tc.fee)}
			if tc.feeGranter != nil {
				options = append(options, user.SetFeeGranter(tc.feeGranter))
			}
			msg := upgradetypes.NewMsgTryUpgrade(s.signer.Address())

			resp, err := s.signer.SubmitTx(s.ctx.GoContext(), []sdk.Msg{msg}, options...)
			require.NoError(t, err)
			require.EqualValues(t, abci.CodeTypeOK, resp.Code)

			got := getRefund(t, resp, tc.wantRefundRecipient.String())
			assert.Equal(t, tc.wantRefund, got)
		})
	}
}

// getRefund returns the amount refunded to the recipient based on the events in the TxResponse.
func getRefund(t *testing.T, resp *sdk.TxResponse, recipient string) (refund int64) {
	assert.NotNil(t, resp)
	transfers := getTransfers(t, resp.Events)
	for _, transfer := range transfers {
		if transfer.recipient == recipient {
			return transfer.amount
		}
	}
	return refund
}

// getTransfers returns all the transfer events in the slice of events.
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
