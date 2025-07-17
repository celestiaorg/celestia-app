package interop

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v5/app"
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

// TestHandleInboundTransfer asserts that inbound transfers to a celestia chain are rejected when they do not contain
// the celestia native token
func (suite *TokenFilterTestSuite) TestHandleInboundTransfer() {
	// setup between celestiaChain and otherChain
	path := ibctesting.NewTransferPath(suite.celestia, suite.simapp)
	suite.coordinator.Setup(path)

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

	// check that the token does not exist on chain A (was rejected)
	voucherDenomTrace := types.ParseDenomTrace(types.GetPrefixedDenom(packet.GetDestPort(), packet.GetDestChannel(), sdk.DefaultBondDenom))
	balance := suite.GetCelestiaApp(suite.celestia).BankKeeper.GetBalance(suite.celestia.GetContext(), suite.celestia.SenderAccount.GetAddress(), voucherDenomTrace.IBCDenom())
	emptyCoin := sdk.NewInt64Coin(voucherDenomTrace.IBCDenom(), 0)
	suite.Require().Equal(emptyCoin, balance)
}
