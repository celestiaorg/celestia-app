package interop

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/suite"
)

type PacketForwardMiddlewareTestSuite struct {
	suite.Suite

	celestia *ibctesting.TestChain
	chainA   *ibctesting.TestChain
	chainB   *ibctesting.TestChain

	pathAToCelestia *ibctesting.Path
	pathCelestiaToB *ibctesting.Path
}

func TestPacketForwardMiddlewareTestSuite(t *testing.T) {
	suite.Run(t, new(PacketForwardMiddlewareTestSuite))
}

func (s *PacketForwardMiddlewareTestSuite) SetupTest() {
	coordinator, celestia, chainA, chainB := SetupTest(s.T())
	s.pathAToCelestia = ibctesting.NewTransferPath(chainA, celestia)
	s.pathCelestiaToB = ibctesting.NewTransferPath(celestia, chainB)

	coordinator.Setup(s.pathAToCelestia)
	coordinator.Setup(s.pathCelestiaToB)

	s.celestia = celestia
	s.chainA = chainA
	s.chainB = chainB
}

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

func (s *PacketForwardMiddlewareTestSuite) GetCelestiaApp(chain *ibctesting.TestChain) *app.App {
	app, ok := chain.App.(*app.App)
	s.Require().True(ok)
	return app
}

func (s *PacketForwardMiddlewareTestSuite) GetSimapp(chain *ibctesting.TestChain) *SimApp {
	app, ok := chain.App.(*SimApp)
	s.Require().True(ok)
	return app
}

// TestPacketForwardMiddlewareTransfer sends a PFM transfer originating from Celestia to ChainA, then back to Celestia and finally to ChainB.
// It verifies that Celestia forwards the packet successfully, the balance of the sender account on Celestia decreases by the amount sent,
// and the balance of the receiver account on ChainB increases by the amount sent.
func (s *PacketForwardMiddlewareTestSuite) TestPacketForwardMiddlewareTransfer() {
	celestiaApp := s.GetCelestiaApp(s.celestia)
	originalCelestiaBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), s.celestia.SenderAccount.GetAddress(), sdk.DefaultBondDenom)

	// Take half of the original balance
	transferAmount := originalCelestiaBalance.Amount.QuoRaw(2)
	timeoutHeight := clienttypes.NewHeight(1, 300)
	coinToSendToB := sdk.NewCoin(sdk.DefaultBondDenom, transferAmount)

	// Forward the packet to ChainB
	secondHopMetaData := &PacketMetadata{
		Forward: &ForwardMetadata{
			Receiver: s.chainB.SenderAccount.GetAddress().String(),
			Channel:  s.pathCelestiaToB.EndpointA.ChannelID,
			Port:     s.pathCelestiaToB.EndpointA.ChannelConfig.PortID,
		},
	}
	nextBz, err := json.Marshal(secondHopMetaData)
	s.Require().NoError(err)
	next := string(nextBz)

	// Send it back to Celestia
	firstHopMetaData := &PacketMetadata{
		Forward: &ForwardMetadata{
			Receiver: s.celestia.SenderAccount.GetAddress().String(),
			Channel:  s.pathAToCelestia.EndpointA.ChannelID,
			Port:     s.pathAToCelestia.EndpointA.ChannelConfig.PortID,
			Next:     &next,
		},
	}
	memo, err := json.Marshal(firstHopMetaData)
	s.Require().NoError(err)

	// Transfer path: Celestia -> ChainA -> Celestia -> ChainB
	msg := transfertypes.NewMsgTransfer(s.pathAToCelestia.EndpointB.ChannelConfig.PortID, s.pathAToCelestia.EndpointB.ChannelID, coinToSendToB, s.celestia.SenderAccount.GetAddress().String(), s.chainA.SenderAccount.GetAddress().String(), timeoutHeight, 0, string(memo))

	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err)

	packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
	s.Require().NoError(err)

	err = ForwardPacket([]*ibctesting.Path{s.pathAToCelestia, s.pathAToCelestia, s.pathCelestiaToB}, packet)
	s.Require().NoError(err)

	sourceBalanceAfter := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), s.celestia.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	s.Require().Equal(originalCelestiaBalance.Amount.Sub(transferAmount), sourceBalanceAfter.Amount)

	ibcDenomTrace := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom(packet.GetDestPort(), packet.GetDestChannel(), sdk.DefaultBondDenom))
	destinationBalanceAfter := s.GetSimapp(s.chainB).BankKeeper.GetBalance(s.chainB.GetContext(), s.chainB.SenderAccount.GetAddress(), ibcDenomTrace.IBCDenom())

	s.Require().Equal(transferAmount, destinationBalanceAfter.Amount)
}

// isPacketToEndpoint checks if a packet is meant for the specified endpoint
func isPacketToEndpoint(endpoint *ibctesting.Endpoint, packet channeltypes.Packet) bool {
	pc := endpoint.Chain.App.GetIBCKeeper().ChannelKeeper.GetPacketCommitment(endpoint.Chain.GetContext(), packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())
	return bytes.Equal(pc, channeltypes.CommitPacket(endpoint.Chain.App.AppCodec(), packet))
}

// relayPacket submits packet to an endpoint and returns either the acknowledgement or another packet
func relayPacket(endpoint *ibctesting.Endpoint, packet channeltypes.Packet) (channeltypes.Packet, []byte, error) {
	if err := endpoint.UpdateClient(); err != nil {
		return channeltypes.Packet{}, nil, err
	}

	res, err := endpoint.RecvPacketWithResult(packet)
	if err != nil {
		return channeltypes.Packet{}, nil, err
	}

	ack, err := ibctesting.ParseAckFromEvents(res.GetEvents())
	if err != nil {
		packet, err = ibctesting.ParsePacketFromEvents(res.GetEvents())
		if err != nil {
			return channeltypes.Packet{}, nil, err
		}
		return packet, nil, nil
	}

	return packet, ack, nil
}

// ForwardPacket forwards a packet through a series of paths and routes the acknowledgement back
func ForwardPacket(paths []*ibctesting.Path, packet channeltypes.Packet) error {
	if len(paths) < 2 {
		return errors.New("path must have at least two hops to forward packet")
	}

	var (
		ack             []byte
		rewindEndpoints = make([]*ibctesting.Endpoint, len(paths))
		packets         = make([]channeltypes.Packet, len(paths))
	)

	// Relay the packet through the paths and store the packets and acknowledgements
	packets[0] = packet
	for idx, path := range paths {
		switch {
		case isPacketToEndpoint(path.EndpointA, packets[idx]):
			packet, packetAck, err := relayPacket(path.EndpointB, packets[idx])
			if err != nil {
				return err
			}
			if len(packetAck) == 0 {
				packets[idx+1] = packet
			} else {
				ack = packetAck
			}
			rewindEndpoints[idx] = path.EndpointA
		case isPacketToEndpoint(path.EndpointB, packets[idx]):
			packet, packetAck, err := relayPacket(path.EndpointA, packets[idx])
			if err != nil {
				return err
			}
			if len(packetAck) == 0 {
				packets[idx+1] = packet
			} else {
				ack = packetAck
			}
			rewindEndpoints[idx] = path.EndpointB
		default:
			return errors.New("packet is for neither endpoint A nor endpoint B")
		}
	}

	if len(ack) == 0 {
		return errors.New("no acknowledgement received from the last packet")
	}

	// Now we route the acknowledgements back
	for i := len(rewindEndpoints) - 1; i >= 0; i-- {
		if err := rewindEndpoints[i].UpdateClient(); err != nil {
			return err
		}

		res, err := AcknowledgePacket(rewindEndpoints[i], packets[i], ack)
		if err != nil {
			return err
		}
		// On endpoint at index 0 ack has reached the source chain
		// so we no longer need to parse it
		if i > 0 {
			ack, err = ibctesting.ParseAckFromEvents(res.GetEvents())
			if err != nil {
				return err
			}
		}
		rewindEndpoints[i].Chain.Coordinator.CommitBlock()
	}
	return nil
}

// AcknowledgePacket acknowledges a packet and returns the result
func AcknowledgePacket(endpoint *ibctesting.Endpoint, packet channeltypes.Packet, ack []byte) (*abci.ExecTxResult, error) {
	packetKey := host.PacketAcknowledgementKey(packet.GetDestPort(), packet.GetDestChannel(), packet.GetSequence())
	proof, proofHeight := endpoint.Counterparty.QueryProof(packetKey)
	ackMsg := channeltypes.NewMsgAcknowledgement(packet, ack, proof, proofHeight, endpoint.Chain.SenderAccount.GetAddress().String())

	return endpoint.Chain.SendMsgs(ackMsg)
}
