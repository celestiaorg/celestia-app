package interop

import (
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	hooktypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	"github.com/celestiaorg/celestia-app/v10/app/params"
	forwardingtypes "github.com/celestiaorg/celestia-app/v10/x/forwarding/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
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

// TestMsgForwardCustomHookRoutesToChosenIGP proves the x/forwarding custom_hook_id
// change: a forward carrying custom_hook_id pays the chosen IGP, while an otherwise
// identical forward without it uses the mailbox default hook (here a free noop hook)
// and pays no IGP. Same route, same token — only the hook differs.
func (s *ForwardingIntegrationTestSuite) TestMsgForwardCustomHookRoutesToChosenIGP() {
	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Warp route: collateral(utia) on Celestia -> synthetic on chainA.
	ismCel := s.SetupNoopISM(s.celestia)
	mailboxCel := s.SetupMailBox(s.celestia, ismCel, TestCelestiaDomainID)
	collatToken := s.CreateCollateralToken(s.celestia, ismCel, mailboxCel, params.BondDenom)
	ismA := s.SetupNoopISM(s.chainA)
	_ = s.SetupMailBox(s.chainA, ismA, TestChainADomainID)
	synToken := s.CreateSyntheticToken(s.chainA, ismA, mailboxCel)
	s.EnrollRemoteRouter(s.celestia, collatToken, TestChainADomainID, synToken.String())

	// Our IGP with a positive quoted fee for the destination domain.
	ourIGP := s.createIGP(params.BondDenom)
	s.setIGPGas(ourIGP, TestChainADomainID, math.NewInt(200000)) // fee = 200000 * 1e10 * 1 / 1e10 = 200000 utia
	destRecipient := MakeRecipient32(s.chainA.SenderAccount.GetAddress())

	// --- Case A: forward WITH custom_hook_id = our IGP ---
	fwdA := s.deriveForwardAddress(TestChainADomainID, destRecipient, collatToken)
	s.fundAddress(s.celestia, fwdA, sdk.NewCoin(params.BondDenom, math.NewInt(1000)))
	msgA := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(), fwdA.String(),
		TestChainADomainID, RecipientToHex(destRecipient).String(), collatToken.String(),
		sdk.NewCoin(params.BondDenom, math.NewInt(500000)),
	)
	msgA.CustomHookId = ourIGP.String()
	resA, err := s.celestia.SendMsgs(msgA)
	s.Require().NoError(err)

	gpA := extractGasPayment(resA.Events)
	s.Require().NotNil(gpA, "custom-hook forward must emit a gas payment")
	s.Equal(ourIGP.String(), gpA.IgpId.String(), "fee must be paid to the custom IGP")
	s.Equal(TestChainADomainID, gpA.Destination)
	s.NotEmpty(gpA.Payment, "payment must be non-zero")

	// --- Case B: identical forward WITHOUT custom_hook_id -> default (noop) hook, no IGP payment ---
	destRecipientB := MakeRecipient32(s.celestia.SenderAccount.GetAddress())
	fwdB := s.deriveForwardAddress(TestChainADomainID, destRecipientB, collatToken)
	s.fundAddress(s.celestia, fwdB, sdk.NewCoin(params.BondDenom, math.NewInt(1000)))
	msgB := s.newForwardMsg(fwdB, TestChainADomainID, destRecipientB, collatToken)
	resB, err := s.celestia.SendMsgs(msgB)
	s.Require().NoError(err)
	s.Require().Nil(extractGasPayment(resB.Events), "default-hook forward must not pay any IGP")

	// Sanity: both forwards actually dispatched and drained their addresses.
	s.True(celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), fwdA, params.BondDenom).IsZero())
	s.True(celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), fwdB, params.BondDenom).IsZero())
}
