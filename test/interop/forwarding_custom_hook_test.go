package interop

import (
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	hooktypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/celestiaorg/celestia-app/v10/app/params"
	forwardingtypes "github.com/celestiaorg/celestia-app/v10/x/forwarding/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
)

// createIGP creates an Interchain Gas Paymaster owned by the chain's sender.
func (s *ForwardingIntegrationTestSuite) createIGP(denom string) util.HexAddress {
	msg := &hooktypes.MsgCreateIgp{
		Owner: s.celestia.SenderAccount.GetAddress().String(),
		Denom: denom,
	}
	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err)
	var resp hooktypes.MsgCreateIgpResponse
	s.Require().NoError(unmarshalMsgResponses(s.celestia.Codec, res.GetData(), &resp))
	return resp.Id
}

// setIGPGas configures a positive quoted fee for the destination domain on the given IGP.
func (s *ForwardingIntegrationTestSuite) setIGPGas(igpID util.HexAddress, domain uint32, overhead math.Int) {
	msg := &hooktypes.MsgSetDestinationGasConfig{
		Owner: s.celestia.SenderAccount.GetAddress().String(),
		IgpId: igpID,
		DestinationGasConfig: &hooktypes.DestinationGasConfig{
			RemoteDomain: domain,
			GasOracle:    &hooktypes.GasOracle{TokenExchangeRate: math.NewInt(1), GasPrice: math.NewInt(1e10)},
			GasOverhead:  overhead,
		},
	}
	_, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err)
}

// extractGasPayment returns the EventGasPayment emitted by a forward, or nil if none.
func extractGasPayment(events []abci.Event) *hooktypes.EventGasPayment {
	for _, evt := range events {
		if evt.Type != proto.MessageName(&hooktypes.EventGasPayment{}) {
			continue
		}
		typed, err := sdk.ParseTypedEvent(evt)
		if err != nil {
			continue
		}
		if gp, ok := typed.(*hooktypes.EventGasPayment); ok {
			return gp
		}
	}
	return nil
}

// createNoopHook creates a post-dispatch hook that charges no fee.
func (s *ForwardingIntegrationTestSuite) createNoopHook(chain *ibctesting.TestChain) util.HexAddress {
	msg := &hooktypes.MsgCreateNoopHook{
		Owner: chain.SenderAccount.GetAddress().String(),
	}
	res, err := chain.SendMsgs(msg)
	s.Require().NoError(err)
	var resp hooktypes.MsgCreateNoopHookResponse
	s.Require().NoError(unmarshalMsgResponses(chain.Codec, res.GetData(), &resp))
	return resp.Id
}

// setupMailboxWithDefaultHook creates a mailbox with the given default hook.
func (s *ForwardingIntegrationTestSuite) setupMailboxWithDefaultHook(chain *ibctesting.TestChain, ismID, defaultHook util.HexAddress, domain uint32) util.HexAddress {
	requiredHook := s.createNoopHook(chain)
	msg := &coretypes.MsgCreateMailbox{
		Owner:        chain.SenderAccount.GetAddress().String(),
		LocalDomain:  domain,
		DefaultIsm:   ismID,
		DefaultHook:  &defaultHook,
		RequiredHook: &requiredHook,
	}
	res, err := chain.SendMsgs(msg)
	s.Require().NoError(err)
	var resp coretypes.MsgCreateMailboxResponse
	s.Require().NoError(unmarshalMsgResponses(chain.Codec, res.GetData(), &resp))
	return resp.Id
}

// setupPaidRoute creates a route whose mailbox default IGP charges a fee.
func (s *ForwardingIntegrationTestSuite) setupPaidRoute() (token, igp util.HexAddress) {
	igp = s.createIGP(params.BondDenom)
	// fee = gasOverhead * gasPrice * exchangeRate / 1e10 = 200000 * 1e10 * 1 / 1e10.
	s.setIGPGas(igp, TestChainADomainID, math.NewInt(200000))

	ismCel := s.SetupNoopISM(s.celestia)
	mailboxCel := s.setupMailboxWithDefaultHook(s.celestia, ismCel, igp, TestCelestiaDomainID)
	token = s.CreateCollateralToken(s.celestia, ismCel, mailboxCel, params.BondDenom)

	ismA := s.SetupNoopISM(s.chainA)
	_ = s.SetupMailBox(s.chainA, ismA, TestChainADomainID)
	synToken := s.CreateSyntheticToken(s.chainA, ismA, mailboxCel)
	s.EnrollRemoteRouter(s.celestia, token, TestChainADomainID, synToken.String())
	return token, igp
}

// newFundedForward creates and funds a valid forwarding request.
func (s *ForwardingIntegrationTestSuite) newFundedForward(token util.HexAddress, recipient []byte, deposit, maxIgpFee math.Int) (sdk.AccAddress, *forwardingtypes.MsgForward) {
	addr := s.deriveForwardAddress(TestChainADomainID, recipient, token)
	s.fundAddress(s.celestia, addr, sdk.NewCoin(params.BondDenom, deposit))

	msg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(), addr.String(),
		TestChainADomainID, RecipientToHex(recipient).String(), token.String(),
		sdk.NewCoin(params.BondDenom, maxIgpFee),
	)
	return addr, msg
}

// TestMsgForwardUsesOnlyMailboxDefaultHook verifies that forwarding pays the
// mailbox default IGP and rejects a caller-supplied hook before moving the deposit.
func (s *ForwardingIntegrationTestSuite) TestMsgForwardUsesOnlyMailboxDefaultHook() {
	bank := s.GetCelestiaApp(s.celestia).BankKeeper
	token, defaultIGP := s.setupPaidRoute()
	deposit := math.NewInt(1000)

	s.Run("empty hook fields pay the mailbox default IGP", func() {
		recipient := MakeRecipient32(s.chainA.SenderAccount.GetAddress())
		fwd, msg := s.newFundedForward(token, recipient, deposit, math.NewInt(500000))

		res, err := s.celestia.SendMsgs(msg)
		s.Require().NoError(err)

		gasPayment := extractGasPayment(res.Events)
		s.Require().NotNil(gasPayment, "a forward must pay for its delivery")
		s.Equal(defaultIGP.String(), gasPayment.IgpId.String(), "the fee must reach the mailbox default IGP")
		s.Equal(TestChainADomainID, gasPayment.Destination)
		s.NotEmpty(gasPayment.Payment, "the payment must be non-zero")
		s.True(bank.GetBalance(s.celestia.GetContext(), fwd, params.BondDenom).IsZero(),
			"the deposit must be dispatched")
	})

	s.Run("caller-supplied free hook is rejected without moving the deposit", func() {
		// A free hook could dispatch without funding delivery.
		recipient := MakeRecipient32(s.celestia.SenderAccount.GetAddress())
		fwd, msg := s.newFundedForward(token, recipient, deposit, math.ZeroInt())
		msg.CustomHookId = s.createNoopHook(s.celestia).String()

		_, err := s.celestia.SendMsgs(msg)
		// ABCI errors do not preserve Go error chains.
		s.Require().ErrorContains(err, forwardingtypes.ErrCustomHookNotAllowed.Error())
		s.Equal(deposit, bank.GetBalance(s.celestia.GetContext(), fwd, params.BondDenom).Amount,
			"a rejected forward must leave the deposit untouched")
	})
}
