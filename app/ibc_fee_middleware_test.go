package app

import (
	"encoding/json"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	"github.com/stretchr/testify/require"
)

// testIBCAmount is the standard test amount for IBC middleware tests.
const testIBCAmount = "1000000"

// testNormalAddress is a valid bech32 address for testing transfers to non-fee addresses.
var testNormalAddress = sdk.AccAddress("test_normal_addr____").String()

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

func createFeeAddressTransferPacket(denom, receiver string) channeltypes.Packet {
	data := transfertypes.FungibleTokenPacketData{
		Denom:    denom,
		Amount:   testIBCAmount,
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

// TestFeeAddressOnRecvPacketAllowsUtiaToNormalAddress verifies that utia sent to
// a normal (non-fee) address passes through to the wrapped module.
func TestFeeAddressOnRecvPacketAllowsUtiaToNormalAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewFeeAddressIBCMiddleware(mockApp, log.NewNopLogger())

	packet := createFeeAddressTransferPacket(appconsts.BondDenom, testNormalAddress)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.True(t, mockApp.onRecvPacketCalled, "wrapped module should be called")
	require.True(t, ack.Success(), "acknowledgement should be successful")
}

// TestFeeAddressOnRecvPacketAllowsNonUtiaToNormalAddress verifies that non-utia tokens
// sent to a normal (non-fee) address pass through to the wrapped module.
func TestFeeAddressOnRecvPacketAllowsNonUtiaToNormalAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewFeeAddressIBCMiddleware(mockApp, log.NewNopLogger())

	packet := createFeeAddressTransferPacket("uosmo", testNormalAddress)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.True(t, mockApp.onRecvPacketCalled, "wrapped module should be called")
	require.True(t, ack.Success(), "acknowledgement should be successful")
}

// TestFeeAddressOnRecvPacketAllowsUtiaDirectToFeeAddress verifies that native utia
// sent directly to the fee address is allowed.
func TestFeeAddressOnRecvPacketAllowsUtiaDirectToFeeAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewFeeAddressIBCMiddleware(mockApp, log.NewNopLogger())

	// Direct utia (not prefixed)
	packet := createFeeAddressTransferPacket(appconsts.BondDenom, feeaddresstypes.FeeAddressBech32)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.True(t, mockApp.onRecvPacketCalled, "wrapped module should be called for utia to fee address")
	require.True(t, ack.Success(), "acknowledgement should be successful")
}

// TestFeeAddressOnRecvPacketAllowsUtiaReturnToFeeAddress verifies that utia returning
// from another chain (with IBC prefix) can be sent to the fee address.
func TestFeeAddressOnRecvPacketAllowsUtiaReturnToFeeAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewFeeAddressIBCMiddleware(mockApp, log.NewNopLogger())

	// Returning utia has a prefixed denom like "transfer/channel-0/utia"
	prefixedUtia := "transfer/channel-0/" + appconsts.BondDenom
	packet := createFeeAddressTransferPacket(prefixedUtia, feeaddresstypes.FeeAddressBech32)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.True(t, mockApp.onRecvPacketCalled, "wrapped module should be called for returning utia to fee address")
	require.True(t, ack.Success(), "acknowledgement should be successful")
}

// TestFeeAddressOnRecvPacketRejectsNonUtiaToFeeAddress verifies that non-utia tokens
// sent to the fee address are rejected with an error acknowledgement.
func TestFeeAddressOnRecvPacketRejectsNonUtiaToFeeAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewFeeAddressIBCMiddleware(mockApp, log.NewNopLogger())

	packet := createFeeAddressTransferPacket("uosmo", feeaddresstypes.FeeAddressBech32)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.False(t, mockApp.onRecvPacketCalled, "wrapped module should NOT be called")
	require.False(t, ack.Success(), "acknowledgement should be an error")
}

// TestFeeAddressOnRecvPacketRejectsIBCDenomToFeeAddress verifies that IBC-prefixed
// tokens (like ibc/HASH...) sent to the fee address are rejected.
func TestFeeAddressOnRecvPacketRejectsIBCDenomToFeeAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewFeeAddressIBCMiddleware(mockApp, log.NewNopLogger())

	// IBC denom format for foreign tokens
	ibcDenom := "transfer/channel-0/uatom"
	packet := createFeeAddressTransferPacket(ibcDenom, feeaddresstypes.FeeAddressBech32)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.False(t, mockApp.onRecvPacketCalled, "wrapped module should NOT be called")
	require.False(t, ack.Success(), "acknowledgement should be an error")
}

// TestFeeAddressOnRecvPacketPassesThroughNonTransferPackets verifies that non-transfer
// packets (malformed data) are passed through to the wrapped module.
func TestFeeAddressOnRecvPacketPassesThroughNonTransferPackets(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewFeeAddressIBCMiddleware(mockApp, log.NewNopLogger())

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

// TestFeeAddressOnRecvPacketAllowsMultiHopUtiaReturnToFeeAddress verifies that utia
// returning from multiple hops (e.g., Celestia -> Chain A -> Chain B -> Celestia)
// can be sent to the fee address. The denom will have multiple IBC prefixes.
func TestFeeAddressOnRecvPacketAllowsMultiHopUtiaReturnToFeeAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewFeeAddressIBCMiddleware(mockApp, log.NewNopLogger())

	// Multi-hop returning utia has nested prefixes like "transfer/channel-0/transfer/channel-1/utia"
	multiHopUtia := "transfer/channel-0/transfer/channel-1/" + appconsts.BondDenom
	packet := createFeeAddressTransferPacket(multiHopUtia, feeaddresstypes.FeeAddressBech32)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.True(t, mockApp.onRecvPacketCalled, "wrapped module should be called for multi-hop returning utia to fee address")
	require.True(t, ack.Success(), "acknowledgement should be successful")
}

// TestFeeAddressOnRecvPacketRejectsMultiHopNonUtiaToFeeAddress verifies that foreign tokens
// with multi-hop IBC prefixes (e.g., uatom that traveled through multiple chains)
// are still rejected when sent to the fee address.
func TestFeeAddressOnRecvPacketRejectsMultiHopNonUtiaToFeeAddress(t *testing.T) {
	mockApp := &mockIBCModule{
		returnAck: channeltypes.NewResultAcknowledgement([]byte("success")),
	}
	middleware := NewFeeAddressIBCMiddleware(mockApp, log.NewNopLogger())

	// Multi-hop foreign token like "transfer/channel-0/transfer/channel-1/uatom"
	multiHopForeign := "transfer/channel-0/transfer/channel-1/uatom"
	packet := createFeeAddressTransferPacket(multiHopForeign, feeaddresstypes.FeeAddressBech32)

	ctx := sdk.Context{}
	ack := middleware.OnRecvPacket(ctx, packet, nil)

	require.False(t, mockApp.onRecvPacketCalled, "wrapped module should NOT be called for multi-hop foreign token to fee address")
	require.False(t, ack.Success(), "acknowledgement should be an error")
}
