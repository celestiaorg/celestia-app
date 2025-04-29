package tokenfilter_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"

	"github.com/celestiaorg/celestia-app/v4/x/tokenfilter"
)

func TestOnRecvPacket(t *testing.T) {
	data := transfertypes.NewFungibleTokenPacketData("transfer/channel-0/utia", math.NewInt(100).String(), "alice", "bob", "gm")
	packet := channeltypes.NewPacket(data.GetBytes(), 1, "transfer", "channel-0", "transfer", "channel-1", clienttypes.Height{}, 10000)
	packetFromOtherChain := channeltypes.NewPacket(data.GetBytes(), 1, "transfer", "channel-1", "transfer", "channel-0", clienttypes.Height{}, 10000)
	randomPacket := channeltypes.NewPacket([]byte{1, 2, 3, 4}, 1, "port", "channel-99", "port", "channel-100", clienttypes.Height{}, 10000)

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
	_ sdk.Context,
	_ channeltypes.Order,
	_ []string,
	_ string,
	_ string,
	_ *capabilitytypes.Capability,
	_ channeltypes.Counterparty,
	_ string,
) (string, error) {
	m.t.Fatalf("unexpected call to OnChanOpenInit")
	return "", nil
}

func (m *MockIBCModule) OnChanOpenTry(
	_ sdk.Context,
	_ channeltypes.Order,
	_ []string,
	_,
	_ string,
	_ *capabilitytypes.Capability,
	_ channeltypes.Counterparty,
	_ string,
) (version string, err error) {
	m.t.Fatalf("unexpected call to OnChanOpenTry")
	return "", nil
}

func (m *MockIBCModule) OnChanOpenAck(
	_ sdk.Context,
	_,
	_ string,
	_ string,
	_ string,
) error {
	m.t.Fatalf("unexpected call to OnChanOpenAck")
	return nil
}

func (m *MockIBCModule) OnChanOpenConfirm(
	_ sdk.Context,
	_,
	_ string,
) error {
	m.t.Fatalf("unexpected call to OnChanOpenConfirm")
	return nil
}

func (m *MockIBCModule) OnChanCloseInit(
	_ sdk.Context,
	_,
	_ string,
) error {
	m.t.Fatalf("unexpected call to OnChanCloseInit")
	return nil
}

func (m *MockIBCModule) OnChanCloseConfirm(
	_ sdk.Context,
	_,
	_ string,
) error {
	m.t.Fatalf("unexpected call to OnChanCloseConfirm")
	return nil
}

func (m *MockIBCModule) OnRecvPacket(
	_ sdk.Context,
	_ channeltypes.Packet,
	_ sdk.AccAddress,
) exported.Acknowledgement {
	m.called = true
	return channeltypes.NewResultAcknowledgement([]byte{byte(1)})
}

func (m *MockIBCModule) OnAcknowledgementPacket(
	_ sdk.Context,
	_ channeltypes.Packet,
	_ []byte,
	_ sdk.AccAddress,
) error {
	m.t.Fatalf("unexpected call to OnAcknowledgementPacket")
	return nil
}

func (m *MockIBCModule) OnTimeoutPacket(
	_ sdk.Context,
	_ channeltypes.Packet,
	_ sdk.AccAddress,
) error {
	m.t.Fatalf("unexpected call to OnTimeoutPacket")
	return nil
}
