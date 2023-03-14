package tokenfilter_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	transfertypes "github.com/cosmos/ibc-go/v6/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v6/modules/core/04-channel/types"
	"github.com/cosmos/ibc-go/v6/modules/core/exported"

	"github.com/celestiaorg/celestia-app/x/tokenfilter"
)

func TestOnRecvPacket(t *testing.T) {
	data := transfertypes.NewFungibleTokenPacketData("portid/channelid/utia", sdk.NewInt(100).String(), "alice", "bob", "gm")
	packet := channeltypes.NewPacket(data.GetBytes(), 1, "portid", "channelid", "counterpartyportid", "counterpartychannelid", clienttypes.Height{}, 0)
	packetFromOtherChain := channeltypes.NewPacket(data.GetBytes(), 1, "counterpartyportid", "counterpartychannelid", "portid", "channelid", clienttypes.Height{}, 0)
	randomPacket := channeltypes.NewPacket([]byte{1, 2, 3, 4}, 1, "portid", "channelid", "counterpartyportid", "counterpartychannelid", clienttypes.Height{}, 0)

	testCases := []struct {
		name   string
		packet channeltypes.Packet
		err    bool
	}{
		{
			name:   "packet with native token",
			packet: packet,
			err:    false,
		},
		{
			name:   "packet with non-native token",
			packet: packetFromOtherChain,
			err:    true,
		},
		{
			name:   "random packet from a different module",
			packet: randomPacket,
			err:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			module := &MockIBCModule{t: t, called: false}
			middleware := tokenfilter.NewIBCMiddleware(module)

			ctx := sdk.Context{}
			ctx = ctx.WithEventManager(sdk.NewEventManager())
			ack := middleware.OnRecvPacket(
				ctx,
				tc.packet,
				[]byte{},
			)
			if tc.err {
				if module.MethodCalled() {
					t.Fatal("expected error but `OnRecvPacket` was called")
				}
				if ack.Success() {
					t.Fatal("expected error acknowledgement but got success")
				}
			}
		})
	}
}

type MockIBCModule struct {
	t      *testing.T
	called bool
}

func (m *MockIBCModule) MethodCalled() bool {
	return m.called
}

func (m *MockIBCModule) OnChanOpenInit(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID string,
	channelID string,
	channelCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	version string,
) (string, error) {
	m.t.Fatalf("unexpected call to OnChanOpenInit")
	return "", nil
}

func (m *MockIBCModule) OnChanOpenTry(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID,
	channelID string,
	channelCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	counterpartyVersion string,
) (version string, err error) {
	m.t.Fatalf("unexpected call to OnChanOpenTry")
	return "", nil
}

func (m *MockIBCModule) OnChanOpenAck(
	ctx sdk.Context,
	portID,
	channelID string,
	counterpartyChannelID string,
	counterpartyVersion string,
) error {
	m.t.Fatalf("unexpected call to OnChanOpenAck")
	return nil
}

func (m *MockIBCModule) OnChanOpenConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	m.t.Fatalf("unexpected call to OnChanOpenConfirm")
	return nil
}

func (m *MockIBCModule) OnChanCloseInit(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	m.t.Fatalf("unexpected call to OnChanCloseInit")
	return nil
}

func (m *MockIBCModule) OnChanCloseConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	m.t.Fatalf("unexpected call to OnChanCloseConfirm")
	return nil
}

func (m *MockIBCModule) OnRecvPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) exported.Acknowledgement {
	m.called = true
	return channeltypes.NewResultAcknowledgement([]byte{byte(1)})
}

func (m *MockIBCModule) OnAcknowledgementPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	acknowledgement []byte,
	relayer sdk.AccAddress,
) error {
	m.t.Fatalf("unexpected call to OnAcknowledgementPacket")
	return nil
}

func (m *MockIBCModule) OnTimeoutPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) error {
	m.t.Fatalf("unexpected call to OnTimeoutPacket")
	return nil
}
