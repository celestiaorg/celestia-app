package app

import (
	"encoding/json"
	"testing"

	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	burntypes "github.com/celestiaorg/celestia-app/v7/x/burn/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	"github.com/stretchr/testify/require"
)

// mockIBCModule is a mock implementation of porttypes.IBCModule for testing.
type mockIBCModule struct {
	onRecvPacketCalled bool
	returnAck          ibcexported.Acknowledgement
}

func (m *mockIBCModule) OnChanOpenInit(_ sdk.Context, _ channeltypes.Order, _ []string, _ string, _ string, _ *capabilitytypes.Capability, _ channeltypes.Counterparty, _ string) (string, error) {
	return "", nil
}

func (m *mockIBCModule) OnChanOpenTry(_ sdk.Context, _ channeltypes.Order, _ []string, _, _ string, _ *capabilitytypes.Capability, _ channeltypes.Counterparty, _ string) (string, error) {
	return "", nil
}

func (m *mockIBCModule) OnChanOpenAck(_ sdk.Context, _, _, _, _ string) error {
	return nil
}

func (m *mockIBCModule) OnChanOpenConfirm(_ sdk.Context, _, _ string) error {
	return nil
}

func (m *mockIBCModule) OnChanCloseInit(_ sdk.Context, _, _ string) error {
	return nil
}

func (m *mockIBCModule) OnChanCloseConfirm(_ sdk.Context, _, _ string) error {
	return nil
}

func (m *mockIBCModule) OnRecvPacket(_ sdk.Context, _ channeltypes.Packet, _ sdk.AccAddress) ibcexported.Acknowledgement {
	m.onRecvPacketCalled = true
	return m.returnAck
}

func (m *mockIBCModule) OnAcknowledgementPacket(_ sdk.Context, _ channeltypes.Packet, _ []byte, _ sdk.AccAddress) error {
	return nil
}

func (m *mockIBCModule) OnTimeoutPacket(_ sdk.Context, _ channeltypes.Packet, _ sdk.AccAddress) error {
	return nil
}

var _ porttypes.IBCModule = (*mockIBCModule)(nil)

func createTransferPacket(denom, receiver string) channeltypes.Packet {
	data := transfertypes.FungibleTokenPacketData{
		Denom:    denom,
		Amount:   "1000000",
		Sender:   "cosmos1sender",
		Receiver: receiver,
	}
	dataBz, _ := json.Marshal(data)
	return channeltypes.Packet{
		Sequence:           1,
		SourcePort:         "transfer",
		SourceChannel:      "channel-0",
		DestinationPort:    "transfer",
		DestinationChannel: "channel-1",
		Data:               dataBz,
	}
}

// TestOnRecvPacketAllowsUtiaToNormalAddress verifies that utia sent to
// a normal (non-burn) address passes through to the wrapped module.
func TestOnRecvPacketAllowsUtiaToNormalAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewBurnAddressIBCMiddleware(mockApp)

	normalAddr := "celestia1abcdefghijklmnopqrstuvwxyz123456"
	packet := createTransferPacket(appconsts.BondDenom, normalAddr)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.True(t, mockApp.onRecvPacketCalled, "wrapped module should be called")
	require.True(t, ack.Success(), "acknowledgement should be successful")
}

// TestOnRecvPacketAllowsNonUtiaToNormalAddress verifies that non-utia tokens
// sent to a normal (non-burn) address pass through to the wrapped module.
func TestOnRecvPacketAllowsNonUtiaToNormalAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewBurnAddressIBCMiddleware(mockApp)

	normalAddr := "celestia1abcdefghijklmnopqrstuvwxyz123456"
	packet := createTransferPacket("uosmo", normalAddr)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.True(t, mockApp.onRecvPacketCalled, "wrapped module should be called")
	require.True(t, ack.Success(), "acknowledgement should be successful")
}

// TestOnRecvPacketAllowsUtiaDirectToBurnAddress verifies that native utia
// sent directly to the burn address is allowed.
func TestOnRecvPacketAllowsUtiaDirectToBurnAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewBurnAddressIBCMiddleware(mockApp)

	// Direct utia (not prefixed)
	packet := createTransferPacket(appconsts.BondDenom, burntypes.BurnAddressBech32)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.True(t, mockApp.onRecvPacketCalled, "wrapped module should be called for utia to burn address")
	require.True(t, ack.Success(), "acknowledgement should be successful")
}

// TestOnRecvPacketAllowsUtiaReturnToBurnAddress verifies that utia returning
// from another chain (with IBC prefix) can be sent to the burn address.
func TestOnRecvPacketAllowsUtiaReturnToBurnAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewBurnAddressIBCMiddleware(mockApp)

	// Returning utia has a prefixed denom like "transfer/channel-0/utia"
	prefixedUtia := "transfer/channel-0/" + appconsts.BondDenom
	packet := createTransferPacket(prefixedUtia, burntypes.BurnAddressBech32)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.True(t, mockApp.onRecvPacketCalled, "wrapped module should be called for returning utia to burn address")
	require.True(t, ack.Success(), "acknowledgement should be successful")
}

// TestOnRecvPacketRejectsNonUtiaToBurnAddress verifies that non-utia tokens
// sent to the burn address are rejected with an error acknowledgement.
func TestOnRecvPacketRejectsNonUtiaToBurnAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewBurnAddressIBCMiddleware(mockApp)

	packet := createTransferPacket("uosmo", burntypes.BurnAddressBech32)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.False(t, mockApp.onRecvPacketCalled, "wrapped module should NOT be called")
	require.False(t, ack.Success(), "acknowledgement should be an error")
}

// TestOnRecvPacketRejectsIBCDenomToBurnAddress verifies that IBC-prefixed
// tokens (like ibc/HASH...) sent to the burn address are rejected.
func TestOnRecvPacketRejectsIBCDenomToBurnAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewBurnAddressIBCMiddleware(mockApp)

	// IBC denom format for foreign tokens
	ibcDenom := "transfer/channel-0/uatom"
	packet := createTransferPacket(ibcDenom, burntypes.BurnAddressBech32)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.False(t, mockApp.onRecvPacketCalled, "wrapped module should NOT be called")
	require.False(t, ack.Success(), "acknowledgement should be an error")
}

// TestOnRecvPacketPassesThroughNonTransferPackets verifies that non-transfer
// packets (malformed data) are passed through to the wrapped module.
func TestOnRecvPacketPassesThroughNonTransferPackets(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewBurnAddressIBCMiddleware(mockApp)

	// Create a packet with non-transfer data
	packet := channeltypes.Packet{
		Sequence:           1,
		SourcePort:         "transfer",
		SourceChannel:      "channel-0",
		DestinationPort:    "transfer",
		DestinationChannel: "channel-1",
		Data:               []byte("not a valid transfer packet"),
	}

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.True(t, mockApp.onRecvPacketCalled, "wrapped module should be called for non-transfer packets")
	require.True(t, ack.Success(), "acknowledgement should be successful")
}
