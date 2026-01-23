package interop

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/suite"
)

type TokenFilterTestSuite struct {
	suite.Suite

	coordinator *ibctesting.Coordinator

	celestia *ibctesting.TestChain
	simapp   *ibctesting.TestChain
}

func TestTokenFilterTestSuite(t *testing.T) {
	suite.Run(t, new(TokenFilterTestSuite))
}

func (suite *TokenFilterTestSuite) SetupTest() {
	coordinator, celestia, simapp, _ := SetupTest(suite.T())

	suite.coordinator = coordinator
	suite.celestia = celestia
	suite.simapp = simapp
}

func (suite *TokenFilterTestSuite) GetCelestiaApp(chain *ibctesting.TestChain) *app.App {
	app, ok := chain.App.(*app.App)
	suite.Require().True(ok)
	return app
}

func (suite *TokenFilterTestSuite) GetSimapp(chain *ibctesting.TestChain) *SimApp {
	app, ok := chain.App.(*SimApp)
	suite.Require().True(ok)
	return app
}

// TestHandleOutboundTransfer asserts that native tokens on a celestia based chain can be transferred to
// another chain and can then return to the original celestia chain
func (suite *TokenFilterTestSuite) TestHandleOutboundTransfer() {
	// setup between celestiaChain and otherChain
	path := ibctesting.NewTransferPath(suite.celestia, suite.simapp)
	suite.coordinator.Setup(path)

	celestiaApp := suite.GetCelestiaApp(suite.celestia)
	originalBalance := celestiaApp.BankKeeper.GetBalance(suite.celestia.GetContext(), suite.celestia.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	// take half of the original balance
	amount := originalBalance.Amount.QuoRaw(2)
	timeoutHeight := clienttypes.NewHeight(1, 110)
	coinToSendToB := sdk.NewCoin(sdk.DefaultBondDenom, amount)

	// send half the users balance from celestiaChain to otherChain
	msg := types.NewMsgTransfer(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, coinToSendToB, suite.celestia.SenderAccount.GetAddress().String(), suite.simapp.SenderAccount.GetAddress().String(), timeoutHeight, 0, "")
	res, err := suite.celestia.SendMsgs(msg)
	suite.Require().NoError(err) // message committed

	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
	suite.Require().NoError(err)

	// relay send
	err = path.RelayPacket(packet)
	suite.Require().NoError(err) // relay committed

	// check that the token exists on chain B
	voucherDenomTrace := types.ParseDenomTrace(types.GetPrefixedDenom(packet.GetDestPort(), packet.GetDestChannel(), sdk.DefaultBondDenom))
	balance := suite.GetSimapp(suite.simapp).BankKeeper.GetBalance(suite.simapp.GetContext(), suite.simapp.SenderAccount.GetAddress(), voucherDenomTrace.IBCDenom())
	coinSentFromAToB := types.GetTransferCoin(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, sdk.DefaultBondDenom, amount)
	suite.Require().Equal(coinSentFromAToB, balance)

	// check that the account on celestiaChain has "amount" less tokens than before
	intermediateBalance := celestiaApp.BankKeeper.GetBalance(suite.celestia.GetContext(), suite.celestia.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	want := originalBalance.Amount.Sub(coinToSendToB.Amount)
	suite.Require().Equal(want, intermediateBalance.Amount)

	// Send the native celestiaChain token on otherChain back to celestiaChain
	msg = types.NewMsgTransfer(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, coinSentFromAToB, suite.simapp.SenderAccount.GetAddress().String(), suite.celestia.SenderAccount.GetAddress().String(), timeoutHeight, 0, "")
	res, err = suite.simapp.SendMsgs(msg)
	suite.Require().NoError(err) // message committed

	packet, err = ibctesting.ParsePacketFromEvents(res.GetEvents())
	suite.Require().NoError(err)

	err = path.RelayPacket(packet)
	suite.Require().NoError(err) // relay committed

	// check that the token was sent back i.e. the new balance is equal to the original balance
	newBalance := celestiaApp.BankKeeper.GetBalance(suite.celestia.GetContext(), suite.celestia.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	suite.Require().Equal(originalBalance, newBalance)
}

// TestHandleInboundTransfer asserts that inbound transfers to a Celestia chain now accept non-native tokens
// and can then be sent back to the original chain. Previously, such transfers were rejected.
func (suite *TokenFilterTestSuite) TestHandleInboundTransfer() {
	// setup between celestiaChain and otherChain
	path := ibctesting.NewTransferPath(suite.celestia, suite.simapp)
	suite.coordinator.Setup(path)

	simApp := suite.GetSimapp(suite.simapp)
	originalBalance := simApp.BankKeeper.GetBalance(suite.simapp.GetContext(), suite.simapp.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	amount, ok := math.NewIntFromString("1000")
	suite.Require().True(ok)
	timeoutHeight := clienttypes.NewHeight(1, 110)
	coinToSendToA := sdk.NewCoin(sdk.DefaultBondDenom, amount)

	// send from otherChain to celestiaChain
	msg := types.NewMsgTransfer(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, coinToSendToA, suite.simapp.SenderAccount.GetAddress().String(), suite.celestia.SenderAccount.GetAddress().String(), timeoutHeight, 0, "")
	res, err := suite.simapp.SendMsgs(msg)
	suite.Require().NoError(err) // message committed

	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
	suite.Require().NoError(err)

	// relay send
	err = path.RelayPacket(packet)
	suite.Require().NoError(err) // relay committed

	// check that the token exists on celestiaChain
	voucherDenomTrace := types.ParseDenomTrace(types.GetPrefixedDenom(packet.GetDestPort(), packet.GetDestChannel(), sdk.DefaultBondDenom))
	balance := suite.GetCelestiaApp(suite.celestia).BankKeeper.GetBalance(suite.celestia.GetContext(), suite.celestia.SenderAccount.GetAddress(), voucherDenomTrace.IBCDenom())
	sentCoin := sdk.NewInt64Coin(voucherDenomTrace.IBCDenom(), 1000)
	suite.Require().Equal(sentCoin, balance)

	// check that the account on simapp has "amount" less tokens than before
	intermediateBalance := simApp.BankKeeper.GetBalance(suite.simapp.GetContext(), suite.simapp.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	want := originalBalance.Amount.Sub(coinToSendToA.Amount)
	suite.Require().Equal(want, intermediateBalance.Amount)

	// Send the token back from celestiaChain to otherChain
	msg = types.NewMsgTransfer(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, sentCoin, suite.celestia.SenderAccount.GetAddress().String(), suite.simapp.SenderAccount.GetAddress().String(), timeoutHeight, 0, "")
	res, err = suite.celestia.SendMsgs(msg)
	suite.Require().NoError(err) // message committed

	packet, err = ibctesting.ParsePacketFromEvents(res.GetEvents())
	suite.Require().NoError(err)

	err = path.RelayPacket(packet)
	suite.Require().NoError(err) // relay committed

	// check that the token was sent back i.e. the new balance is equal to the original balance
	newBalance := simApp.BankKeeper.GetBalance(suite.simapp.GetContext(), suite.simapp.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	suite.Require().Equal(originalBalance, newBalance)

	// check that the celestia balance is 0 after sending back the token
	finalCelestiaBalance := suite.GetCelestiaApp(suite.celestia).BankKeeper.GetBalance(suite.celestia.GetContext(), suite.celestia.SenderAccount.GetAddress(), voucherDenomTrace.IBCDenom())
	suite.Require().Equal(sdk.NewInt64Coin(voucherDenomTrace.IBCDenom(), 0), finalCelestiaBalance)
}

// TestInboundUtiaReturnToFeeAddressAllowed verifies that native utia returning
// to Celestia can be sent to the fee address and will be forwarded to fee collector.
func (suite *TokenFilterTestSuite) TestInboundUtiaReturnToFeeAddressAllowed() {
	// setup between celestiaChain and otherChain
	path := ibctesting.NewTransferPath(suite.celestia, suite.simapp)
	suite.coordinator.Setup(path)

	celestiaApp := suite.GetCelestiaApp(suite.celestia)

	// First, send utia from celestia to simapp
	amount, ok := math.NewIntFromString("1000")
	suite.Require().True(ok)
	timeoutHeight := clienttypes.NewHeight(1, 110)
	coinToSend := sdk.NewCoin(sdk.DefaultBondDenom, amount)

	msg := types.NewMsgTransfer(
		path.EndpointA.ChannelConfig.PortID,
		path.EndpointA.ChannelID,
		coinToSend,
		suite.celestia.SenderAccount.GetAddress().String(),
		suite.simapp.SenderAccount.GetAddress().String(),
		timeoutHeight,
		0,
		"",
	)
	res, err := suite.celestia.SendMsgs(msg)
	suite.Require().NoError(err)

	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
	suite.Require().NoError(err)

	err = path.RelayPacket(packet)
	suite.Require().NoError(err)

	// Now send the utia back from simapp to celestia fee address
	// This should be ALLOWED because it's native utia returning
	voucherDenomTrace := types.ParseDenomTrace(types.GetPrefixedDenom(packet.GetDestPort(), packet.GetDestChannel(), sdk.DefaultBondDenom))
	ibcCoin := sdk.NewInt64Coin(voucherDenomTrace.IBCDenom(), 1000)

	msg = types.NewMsgTransfer(
		path.EndpointB.ChannelConfig.PortID,
		path.EndpointB.ChannelID,
		ibcCoin,
		suite.simapp.SenderAccount.GetAddress().String(),
		feeaddresstypes.FeeAddressBech32, // Sending to fee address
		timeoutHeight,
		0,
		"",
	)
	res, err = suite.simapp.SendMsgs(msg)
	suite.Require().NoError(err)

	packet, err = ibctesting.ParsePacketFromEvents(res.GetEvents())
	suite.Require().NoError(err)

	// Relay - this should succeed because utia is allowed to fee address
	err = path.RelayPacket(packet)
	suite.Require().NoError(err)

	// Verify tokens arrived at fee address (proving IBC transfer was accepted).
	// Note: In production, PrepareProposal would inject a MsgForwardFees tx in the
	// next block to forward these tokens to fee collector. The IBC testing framework
	// doesn't invoke PrepareProposal, so we verify token arrival instead.
	// Full E2E forwarding is tested in x/feeaddress/test/feeaddress_test.go.
	feeAddressBalance := celestiaApp.BankKeeper.GetBalance(suite.celestia.GetContext(), feeaddresstypes.FeeAddress, sdk.DefaultBondDenom)
	suite.Require().Equal(int64(1000), feeAddressBalance.Amount.Int64(), "fee address should have received the utia")
}
