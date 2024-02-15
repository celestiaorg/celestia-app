package app_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/ibc-go/v6/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	ibctesting "github.com/cosmos/ibc-go/v6/testing"
	"github.com/stretchr/testify/suite"
	utils "github.com/celestiaorg/celestia-app/test/tokenfilter"
)

type PacketForwardTestSuit struct {
	suite.Suite

	coordinator *ibctesting.Coordinator

	// Celestia app including the packet forward middleware
	celestiaChain *ibctesting.TestChain

	// Default IBC Simapp
	otherChain *ibctesting.TestChain

	// Another chain to test the packet forward middleware
	anotherChain *ibctesting.TestChain
}

// Target Tests 

// Successful path unwinding from gaia-testnet-1 to celestia-testnet to gaia-testnet-2 (done)
// Proper refunding in a multi-hop IBC flow if any step returns a recv_packet error 
// Ensure Retries On Timeout config works, with the intended number of retry attempts upon hitting the Timeout Period
// Ensure Refund Timeout issues a refund when a forward is in progress for too long
// If Fee Percentage is not set to 0, ensure the proper token amount is claimed from packets and sent to the Community Pool. (ig since this will be introduced in v2 in which we're introducing a global min fee we should )


func (suite *PacketForwardTestSuit) SetupTest() {
	chains := make(map[string]*ibctesting.TestChain)
	suite.coordinator = &ibctesting.Coordinator{
		T:           suite.T(),
		CurrentTime: time.Now(),
		Chains:      chains,
	}
	
	suite.celestiaChain = utils.NewTestChain(suite.T(), suite.coordinator, ibctesting.GetChainID(1))
	suite.otherChain = ibctesting.NewTestChain(suite.T(), suite.coordinator, ibctesting.GetChainID(2))
	suite.anotherChain = ibctesting.NewTestChain(suite.T(), suite.coordinator, ibctesting.GetChainID(3))

	suite.coordinator.Chains[ibctesting.GetChainID(1)] = suite.celestiaChain
	suite.coordinator.Chains[ibctesting.GetChainID(2)] = suite.otherChain
    suite.coordinator.Chains[ibctesting.GetChainID(3)] = suite.anotherChain

}

func NewTransferPaths(celestiaChain, otherChain, anotherChain *ibctesting.TestChain) (*ibctesting.Path, *ibctesting.Path) {
    path1 := ibctesting.NewPath(celestiaChain, otherChain)
    path1.EndpointA.ChannelConfig.PortID = ibctesting.TransferPort
    path1.EndpointB.ChannelConfig.PortID = ibctesting.TransferPort
    path1.EndpointA.ChannelConfig.Version = types.Version
    path1.EndpointB.ChannelConfig.Version = types.Version

    path2 := ibctesting.NewPath(otherChain, anotherChain)
    path2.EndpointA.ChannelConfig.PortID = ibctesting.TransferPort
    path2.EndpointB.ChannelConfig.PortID = ibctesting.TransferPort
    path2.EndpointA.ChannelConfig.Version = types.Version
    path2.EndpointB.ChannelConfig.Version = types.Version

    return path1, path2
}

// TestPacketForwardMiddlewareTransfer asserts that native tokens on a Celestia-based chain can be transferred to
// another chain and then return to the original Celestia chain using the packet forward middleware.
func (suite *PacketForwardTestSuit) TestPacketForwardMiddlewareTransfer() {
	// setup between celestiaChain and otherChain and anotherchain
	path1, path2 := NewTransferPaths(suite.celestiaChain, suite.otherChain, suite.anotherChain)
	suite.coordinator.Setup(path1)
	suite.coordinator.Setup(path2)

	celestiaApp := suite.celestiaChain.App.(*app.App)
	originalBalance := celestiaApp.BankKeeper.GetBalance(suite.celestiaChain.GetContext(), suite.celestiaChain.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	// take half of the original balance
	amount := originalBalance.Amount.QuoRaw(2)
	timeoutHeight := clienttypes.NewHeight(1, 110)
	coinToSendToB := sdk.NewCoin(sdk.DefaultBondDenom, amount)

	// send half the users balance from celestiaChain to anotherchain through otherchian
	msg := types.NewMsgTransfer(path1.EndpointA.ChannelConfig.PortID, path2.EndpointB.ChannelID, coinToSendToB, suite.celestiaChain.SenderAccount.GetAddress().String(), suite.anotherChain.SenderAccount.GetAddress().String(), timeoutHeight, 0, "")
	res, err := suite.celestiaChain.SendMsgs(msg)
	suite.Require().NoError(err) // message committed

	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
	suite.Require().NoError(err)

	// relay send
	err = path1.RelayPacket(packet)
	suite.Require().NoError(err) // relay committed

	// relay from otherchain to anotherchain
	err = path2.RelayPacket(packet)
	suite.Require().NoError(err) // relay committed

	// check that the token exists on chain anotherchain
	voucherDenomTrace := types.ParseDenomTrace(types.GetPrefixedDenom(packet.GetDestPort(), packet.GetDestChannel(), sdk.DefaultBondDenom))
	balance := suite.otherChain.GetSimApp().BankKeeper.GetBalance(suite.otherChain.GetContext(), suite.otherChain.SenderAccount.GetAddress(), voucherDenomTrace.IBCDenom())
	coinSentFromAToB := types.GetTransferCoin(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID, sdk.DefaultBondDenom, amount)
	suite.Require().Equal(coinSentFromAToB, balance)

	// check that the account on celestiaChain has "amount" less tokens than before
	intermediateBalance := celestiaApp.BankKeeper.GetBalance(suite.celestiaChain.GetContext(), suite.celestiaChain.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	want := originalBalance.Amount.Sub(coinToSendToB.Amount)
	suite.Require().Equal(want, intermediateBalance.Amount)

	// Send the native celestiaChain token on otherChain back to celestiaChain
	msg = types.NewMsgTransfer(path2.EndpointB.ChannelConfig.PortID, path2.EndpointB.ChannelID, coinSentFromAToB, suite.otherChain.SenderAccount.GetAddress().String(), suite.celestiaChain.SenderAccount.GetAddress().String(), timeoutHeight, 0, "")
	res, err = suite.otherChain.SendMsgs(msg)
	suite.Require().NoError(err) // message committed

	packet, err = ibctesting.ParsePacketFromEvents(res.GetEvents())
	suite.Require().NoError(err)

	err = path2.RelayPacket(packet)
	suite.Require().NoError(err) // relay committed
	// relay from anotherchain to celestiaChain
	err = path1.RelayPacket(packet)
	suite.Require().NoError(err) // relay committed

	// check that the token was sent back i.e. the new balance is equal to the original balance
	newBalance := celestiaApp.BankKeeper.GetBalance(suite.celestiaChain.GetContext(), suite.celestiaChain.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	suite.Require().Equal(originalBalance, newBalance)
}

func TestPacketForwardTestSuit(t *testing.T) {
	suite.Run(t, new(PacketForwardTestSuit))
}
