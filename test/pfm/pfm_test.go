package pfm

import (
	"bytes"
	"fmt"
	"testing"
	"time"
	// "github.com/celestiaorg/celestia-app/app"
	utils "github.com/celestiaorg/celestia-app/v2/test/tokenfilter"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/ibc-go/v6/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	ibctesting "github.com/cosmos/ibc-go/v6/testing"
	// "github.com/stretchr/testify/require"
	"encoding/json"
	"errors"
	"github.com/celestiaorg/celestia-app/v2/app"
	channeltypes "github.com/cosmos/ibc-go/v6/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v6/modules/core/24-host"
	"github.com/stretchr/testify/suite"
)

type PacketMetadata struct {
	Forward *ForwardMetadata `json:"forward"`
}

type ForwardMetadata struct {
	Receiver       string        `json:"receiver"`
	Port           string        `json:"port"`
	Channel        string        `json:"channel"`
	Timeout        time.Duration `json:"timeout"`
	Retries        *uint8        `json:"retries,omitempty"`
	Next           *string       `json:"next,omitempty"`
	RefundSequence *uint64       `json:"refund_sequence,omitempty"`
}

type PacketForwardTestSuit struct {
	suite.Suite
	coordinator *ibctesting.Coordinator
	// Celestia app including the packet forward middleware
	// Default IBC Simapp
	chainA        *ibctesting.TestChain
	celestiaChain *ibctesting.TestChain
	// Another chain to test the packet forward middleware
	chainB *ibctesting.TestChain
}

func (suite *PacketForwardTestSuit) SetupTest() {
	chains := make(map[string]*ibctesting.TestChain)
	suite.coordinator = &ibctesting.Coordinator{
		T:           suite.T(),
		CurrentTime: time.Now(),
		Chains:      chains,
	}
	suite.celestiaChain = utils.NewTestChain(suite.T(), suite.coordinator, ibctesting.GetChainID(1))
	suite.chainA = NewTestChain(suite.T(), suite.coordinator, ibctesting.GetChainID(2))
	suite.chainB = NewTestChain(suite.T(), suite.coordinator, ibctesting.GetChainID(3))
	suite.coordinator.Chains[ibctesting.GetChainID(1)] = suite.celestiaChain
	suite.coordinator.Chains[ibctesting.GetChainID(2)] = suite.chainA
	suite.coordinator.Chains[ibctesting.GetChainID(3)] = suite.chainB
}

func NewTransferPaths(chain1, chain2, chain3 *ibctesting.TestChain) (*ibctesting.Path, *ibctesting.Path) {
	path1 := ibctesting.NewPath(chain1, chain2)
	path1.EndpointA.ChannelConfig.PortID = ibctesting.TransferPort
	path1.EndpointB.ChannelConfig.PortID = ibctesting.TransferPort
	path1.EndpointA.ChannelConfig.Version = types.Version
	path1.EndpointB.ChannelConfig.Version = types.Version
	path2 := ibctesting.NewPath(chain2, chain3)
	path2.EndpointA.ChannelConfig.PortID = ibctesting.TransferPort
	path2.EndpointB.ChannelConfig.PortID = ibctesting.TransferPort
	path2.EndpointA.ChannelConfig.Version = types.Version
	path2.EndpointB.ChannelConfig.Version = types.Version

	return path1, path2
}

// path1EndpointB -> path1EndpointA -> path1EndpointB  -> path2EndpointB

// TestPacketForwardMiddlewareTransfer asserts that native tokens on a Celestia-based chain can be transferred to
// another chain and then return to the original Celestia chain using the packet forward middleware.
func (suite *PacketForwardTestSuit) TestPacketForwardMiddlewareTransfer() {
	path1, path2 := NewTransferPaths(suite.chainA, suite.celestiaChain, suite.chainB)
	suite.coordinator.Setup(path1)
	suite.coordinator.Setup(path2)

	celestiaApp := suite.celestiaChain.App.(*app.App)
	originalCelestiaBal := celestiaApp.BankKeeper.GetBalance(suite.celestiaChain.GetContext(), suite.celestiaChain.SenderAccount.GetAddress(), sdk.DefaultBondDenom)

	fmt.Println(originalCelestiaBal, "sourceChain original balance (celestia)")
	// take half of the original balance
	amount := originalCelestiaBal.Amount.QuoRaw(2)
	timeoutHeight := clienttypes.NewHeight(1, 300)
	coinToSendToB := sdk.NewCoin(sdk.DefaultBondDenom, amount)

	fmt.Println(path1.EndpointA.ChannelID, "channel id path 1 endpoint A")
	fmt.Println(path1.EndpointB.ChannelID, "channel id path 1 endpoint B")
	fmt.Println(path2.EndpointA.ChannelID, "channel id path 2 endpoint A")
	fmt.Println(path2.EndpointB.ChannelID, "channel id path 2 endpoint B")

	// Create the 'next' structure
	nextStruct :=
		&PacketMetadata{
			Forward: &ForwardMetadata{
				Receiver: suite.chainB.SenderAccount.GetAddress().String(),
				Channel:  path2.EndpointA.ChannelID,
				Port:     path2.EndpointA.ChannelConfig.PortID,
			},
		}

	// Marshal 'next' to get a properly escaped string
	nextBytes, err := json.Marshal(nextStruct)
	suite.Require().NoError(err) // no error
	nextEscaped := string(nextBytes)

	// Create the 'memo' structure, embedding 'next' as a raw JSON string
	memoStruct :=
		&PacketMetadata{
			Forward: &ForwardMetadata{
				Receiver: suite.celestiaChain.SenderAccount.GetAddress().String(),
				Channel:  path1.EndpointA.ChannelID,
				Port:     path1.EndpointA.ChannelConfig.PortID,
				Next:     &nextEscaped,
			},
		}

	// Marshal 'memo' to get the final JSON string
	memoBytes, err := json.Marshal(memoStruct)
	suite.Require().NoError(err)
	memo := string(memoBytes)

	// Now 'memo' contains the correctly formatted and escaped JSON string

	// from celestia to chainA initially but with forwarding message in the memo to end up in chainB
	msg := types.NewMsgTransfer(path1.EndpointB.ChannelConfig.PortID, path1.EndpointB.ChannelID, coinToSendToB, suite.celestiaChain.SenderAccount.GetAddress().String(), suite.chainA.SenderAccount.GetAddress().String(), timeoutHeight, 0, memo)

	res, err := suite.celestiaChain.SendMsgs(msg)
	suite.Require().NoError(err) // message committed

	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
	suite.Require().NoError(err)

	// relay send
	err = ForwardPacket([]*ibctesting.Path{path1, path1, path2}, packet)
	suite.Require().NoError(err) // relay committed

	//	// seqNum, _ := suite.celestiaChain.GetSimApp().IBCKeeper.ChannelKeeper.GetNextSequenceSend(suite.celestiaChain.GetContext(), path1.EndpointB.ChannelConfig.PortID, path1.EndpointB.ChannelID)
	//	// fmt.Println(seqNum, "SEQ NUM")
	//	// packet2 := suite.celestiaChain.GetSimApp().IBCKeeper.ChannelKeeper.GetPacketCommitment(suite.celestiaChain.GetContext(), path1.EndpointB.ChannelConfig.PortID, path1.EndpointB.ChannelID, seqNum)
	//	// suite.Require().NoError(err) // relay committed
	//	// check that the token exists on chain anotherchain
	//	voucherDenomTrace := types.ParseDenomTrace(types.GetPrefixedDenom(packet.GetDestPort(), packet.GetDestChannel(), sdk.DefaultBondDenom))
	//	balance := suite.destinationChain.GetSimApp().BankKeeper.GetBalance(suite.destinationChain.GetContext(), suite.destinationChain.SenderAccount.GetAddress(), voucherDenomTrace.IBCDenom())
	//
	// fmt.Println(balance, "balance another chain")
	// // check that the account on otherchain has "amount" less tokens than before
	// intermediateBalance := suite.sourceChain.GetSimApp().BankKeeper.GetBalance(suite.sourceChain.GetContext(), suite.sourceChain.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	// want := originalBalance.Amount.Sub(coinToSendToB.Amount)
	// fmt.Println(want, "WANT")
	// fmt.Println(intermediateBalance, "INTERMEDIATE BALANCE")
	// suite.Require().Equal(want, intermediateBalance.Amount)
	// fmt.Println(originalBalance, "ORIGINAL BALANCE")
	// // Send the native celestiaChain token on sourceChain back to celestiaChain
	// msg = types.NewMsgTransfer(path2.EndpointB.ChannelConfig.PortID, path2.EndpointB.ChannelID, coinSentFromAToB, suite.sourceChain.SenderAccount.GetAddress().String(), suite.celestiaChain.SenderAccount.GetAddress().String(), timeoutHeight, 0, "")
	// res, err = suite.sourceChain.SendMsgs(msg)
	// suite.Require().NoError(err) // message committed
	// packet, err = ibctesting.ParsePacketFromEvents(res.GetEvents())
	// suite.Require().NoError(err)
	// err = path2.RelayPacket(packet)
	// suite.Require().NoError(err) // relay committed
	// // relay from anotherchain to celestiaChain
	// err = path1.RelayPacket(packet)
	// suite.Require().NoError(err) // relay committed
	// // check that the token was sent back i.e. the new balance is equal to the original balance
	// newBalance := celestiaApp.BankKeeper.GetBalance(suite.celestiaChain.GetContext(), suite.celestiaChain.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	// suite.Require().Equal(originalBalance, newBalance)
}

func TestPacketForwardTestSuit(t *testing.T) {
	suite.Run(t, new(PacketForwardTestSuit))
}

func isPacketToEndpoint(endpoint *ibctesting.Endpoint, packet channeltypes.Packet) bool {
	pc := endpoint.Chain.App.GetIBCKeeper().ChannelKeeper.GetPacketCommitment(endpoint.Chain.GetContext(), packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())
	return bytes.Equal(pc, channeltypes.CommitPacket(endpoint.Chain.App.AppCodec(), packet))
}

// submits packet to endpoint and returns either the acknowledgement or another packet
func relayPacket(endpoint *ibctesting.Endpoint, packet channeltypes.Packet) (channeltypes.Packet, []byte, error) {
	if err := endpoint.UpdateClient(); err != nil {
		return channeltypes.Packet{}, nil, err
	}

	res, err := endpoint.RecvPacketWithResult(packet)
	if err != nil {
		fmt.Println("recv packet error")
		return channeltypes.Packet{}, nil, err
	}

	ack, err := ibctesting.ParseAckFromEvents(res.GetEvents())
	if err != nil {
		packet, err = ibctesting.ParsePacketFromEvents(res.GetEvents())
		if err != nil {
			fmt.Println("parse packet error")
			return channeltypes.Packet{}, nil, err
		}
		return packet, nil, nil
	}

	return packet, ack, nil
}

func ForwardPacket(paths []*ibctesting.Path, packet channeltypes.Packet) error {
	if len(paths) < 2 {
		return errors.New("path must have at least two hops to forward packet")
	}

	var (
		err            error
		ack            []byte
		rewindEndpoint = make([]*ibctesting.Endpoint, len(paths))
		rewindPackets  = make([]channeltypes.Packet, len(paths))
	)

	for idx, path := range paths[:len(paths)-1] {
		fmt.Println(path, "PATH FIRST FORWARD")
		if isPacketToEndpoint(path.EndpointA, packet) {
			packet, ack, err = relayPacket(path.EndpointB, packet)
			if len(ack) > 0 {
				return errors.New("unexpected acknowledgement")
			}
			if err != nil {
				return err
			}
			rewindPackets[idx] = packet
			fmt.Println(packet, "packettt")
			rewindEndpoint[idx] = path.EndpointA
			fmt.Println(rewindEndpoint[idx], "rewind endpoint")
		} else if isPacketToEndpoint(path.EndpointB, packet) {
			fmt.Println("hello from if else block")
			packet, ack, err = relayPacket(path.EndpointA, packet)
			if len(ack) > 0 {
				return errors.New("unexpected acknowledgement")
			}
			if err != nil {
				return err
			}
			rewindPackets[idx] = packet
			fmt.Println(packet, "packettt")
			rewindEndpoint[idx] = path.EndpointB
			fmt.Println(rewindEndpoint[idx], "rewind endpoint")
		} else {
			// this error should be a bit more explicit
			return errors.New("packet is for neither endpoint A or endpoint B")
		}
	}


	finalPath := paths[len(paths)-1]
	if isPacketToEndpoint(finalPath.EndpointA, packet) {
		packet, ack, err = relayPacket(finalPath.EndpointB, packet)
		if err != nil {
			return err
		}
		rewindPackets[len(paths)-1] = packet
		fmt.Println(packet, "packettt")
		rewindEndpoint[len(paths)-1] = finalPath.EndpointA
		fmt.Println(rewindEndpoint[len(paths)-1], "rewind endpoint")
	} else if isPacketToEndpoint(finalPath.EndpointB, packet) {
		packet, ack, err = relayPacket(finalPath.EndpointA, packet)
		if err != nil {
			return err
		}
		rewindPackets[len(paths)-1] = packet
		fmt.Println(packet, "packettt")
		rewindEndpoint[len(paths)-1] = finalPath.EndpointB
		fmt.Println(rewindEndpoint[len(paths)-1], "rewind endpoint")
	} else {
		return errors.New("packet is for neither endpoint A or endpoint B")
	}

	fmt.Println(ack, "ACK JUST RECEIVED")

	fmt.Println("rewind acknowledgements")
	fmt.Println(rewindEndpoint, "rewind endpoint")
	fmt.Println(rewindPackets, "rewind packets")
	// now we relay to the final destination and route the acknowledgements back

	for i := len(rewindEndpoint) - 1; i >= 0; i-- {
		fmt.Println(i, "index")
		fmt.Println(ack, "SHOULD BE UPDATED")
		fmt.Println(rewindPackets[i], "rewind packet")
		fmt.Println(rewindEndpoint[i], "rewind endpoint")
		res, err := AcknowledgePacket(rewindEndpoint[i], rewindPackets[i], ack)
		if err != nil {
			return err
		}
		ackk, err := ibctesting.ParseAckFromEvents(res.GetEvents())
		fmt.Println(ackk, "ACK UPDATED")
		if err != nil {
			return err
		}

		fmt.Println(ack, "ACK Updated")
		fmt.Println(i, "index")
	}
	return nil
}

func AcknowledgePacket(endpoint *ibctesting.Endpoint, packet channeltypes.Packet, ack []byte) (*sdk.Result, error) {
	packetKey := host.PacketAcknowledgementKey(packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())
	proof, proofHeight := endpoint.Counterparty.QueryProof(packetKey)

	ackMsg := channeltypes.NewMsgAcknowledgement(packet, ack, proof, proofHeight, endpoint.Chain.SenderAccount.GetAddress().String())

	return endpoint.Chain.SendMsgs(ackMsg)
}
