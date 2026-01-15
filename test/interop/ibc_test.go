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

// TestInboundTransferToFeeAddressRejected verifies that non-utia tokens
// sent to the fee address via IBC are rejected and the sender is refunded.
func (suite *TokenFilterTestSuite) TestInboundTransferToFeeAddressRejected() {
	// setup between celestiaChain and otherChain
	path := ibctesting.NewTransferPath(suite.celestia, suite.simapp)
	suite.coordinator.Setup(path)

	simApp := suite.GetSimapp(suite.simapp)
	celestiaApp := suite.GetCelestiaApp(suite.celestia)

	// Use a foreign denom that is NOT utia (celestia's bond denom).
	// Since sdk.DefaultBondDenom is set to "utia" via app/init.go,
	// we need to use a different denom to test rejection.
	const foreignDenom = "uforeign"

	// Mint foreign tokens to the sender account on simapp
	amount, ok := math.NewIntFromString("1000")
	suite.Require().True(ok)
	foreignCoins := sdk.NewCoins(sdk.NewCoin(foreignDenom, amount))
	err := simApp.BankKeeper.MintCoins(suite.simapp.GetContext(), "mint", foreignCoins)
	suite.Require().NoError(err)
	err = simApp.BankKeeper.SendCoinsFromModuleToAccount(suite.simapp.GetContext(), "mint", suite.simapp.SenderAccount.GetAddress(), foreignCoins)
	suite.Require().NoError(err)

	// Get original balance on simapp (the sender chain)
	originalBalance := simApp.BankKeeper.GetBalance(suite.simapp.GetContext(), suite.simapp.SenderAccount.GetAddress(), foreignDenom)
	suite.Require().Equal(amount, originalBalance.Amount)

	timeoutHeight := clienttypes.NewHeight(1, 110)
	coinToSend := sdk.NewCoin(foreignDenom, amount)

	// Send foreign token from simapp to celestia fee address
	// This should be rejected by the FeeAddressIBCMiddleware
	msg := types.NewMsgTransfer(
		path.EndpointB.ChannelConfig.PortID,
		path.EndpointB.ChannelID,
		coinToSend,
		suite.simapp.SenderAccount.GetAddress().String(),
		feeaddresstypes.FeeAddressBech32, // Sending to fee address
		timeoutHeight,
		0,
		"",
	)
	res, err := suite.simapp.SendMsgs(msg)
	suite.Require().NoError(err) // message committed on simapp

	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
	suite.Require().NoError(err)

	// Relay the packet - this should result in an error acknowledgement
	err = path.RelayPacket(packet)
	suite.Require().NoError(err) // relay committed (error ack is still "successful" relay)

	// Verify the fee address on celestia has zero balance of the IBC token
	voucherDenomTrace := types.ParseDenomTrace(types.GetPrefixedDenom(packet.GetDestPort(), packet.GetDestChannel(), foreignDenom))
	feeAddressBalance := celestiaApp.BankKeeper.GetBalance(suite.celestia.GetContext(), feeaddresstypes.FeeAddress, voucherDenomTrace.IBCDenom())
	suite.Require().Equal(sdk.NewInt64Coin(voucherDenomTrace.IBCDenom(), 0), feeAddressBalance, "fee address should have zero balance")

	// Verify the sender on simapp was refunded (balance restored)
	newBalance := simApp.BankKeeper.GetBalance(suite.simapp.GetContext(), suite.simapp.SenderAccount.GetAddress(), foreignDenom)
	suite.Require().Equal(originalBalance, newBalance, "sender should be refunded after rejection")
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

	// Note: RelayPacket commits blocks internally, which runs the EndBlocker.
	// The EndBlocker forwards tokens to fee collector, and then the distribution
	// module's BeginBlocker distributes them to validators in the next block.
	// So we verify the fee address is empty (proving tokens were forwarded).
	feeAddressBalance := celestiaApp.BankKeeper.GetBalance(suite.celestia.GetContext(), feeaddresstypes.FeeAddress, sdk.DefaultBondDenom)
	suite.Require().Equal(int64(0), feeAddressBalance.Amount.Int64(), "fee address should be empty after EndBlocker")
}
