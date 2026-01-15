package app

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
)

var _ porttypes.IBCModule = (*FeeAddressIBCMiddleware)(nil)

// FeeAddressIBCMiddleware rejects inbound IBC transfers of non-utia tokens
// to the fee address. This prevents foreign tokens from getting permanently
// stuck at the fee address since the EndBlocker only forwards utia.
//
// When a transfer is rejected, an error acknowledgement is returned, causing
// the source chain to refund the sender.
//
// Note: This middleware only validates standard IBC transfers. Bypass vectors exist:
// - ICA host messages bypass ante handlers and this middleware
// - Hyperlane MsgProcessMessage bypasses this middleware
// Non-utia tokens sent via these paths would be permanently stuck (not forwarded, not stolen).
type FeeAddressIBCMiddleware struct {
	app porttypes.IBCModule
}

// NewFeeAddressIBCMiddleware creates a new FeeAddressIBCMiddleware.
func NewFeeAddressIBCMiddleware(app porttypes.IBCModule) FeeAddressIBCMiddleware {
	return FeeAddressIBCMiddleware{app: app}
}

// OnChanOpenInit implements the IBCModule interface.
func (m FeeAddressIBCMiddleware) OnChanOpenInit(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID string,
	channelID string,
	chanCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	version string,
) (string, error) {
	return m.app.OnChanOpenInit(ctx, order, connectionHops, portID, channelID, chanCap, counterparty, version)
}

// OnChanOpenTry implements the IBCModule interface.
func (m FeeAddressIBCMiddleware) OnChanOpenTry(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID,
	channelID string,
	chanCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	counterpartyVersion string,
) (string, error) {
	return m.app.OnChanOpenTry(ctx, order, connectionHops, portID, channelID, chanCap, counterparty, counterpartyVersion)
}

// OnChanOpenAck implements the IBCModule interface.
func (m FeeAddressIBCMiddleware) OnChanOpenAck(
	ctx sdk.Context,
	portID,
	channelID string,
	counterpartyChannelID string,
	counterpartyVersion string,
) error {
	return m.app.OnChanOpenAck(ctx, portID, channelID, counterpartyChannelID, counterpartyVersion)
}

// OnChanOpenConfirm implements the IBCModule interface.
func (m FeeAddressIBCMiddleware) OnChanOpenConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	return m.app.OnChanOpenConfirm(ctx, portID, channelID)
}

// OnChanCloseInit implements the IBCModule interface.
func (m FeeAddressIBCMiddleware) OnChanCloseInit(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	return m.app.OnChanCloseInit(ctx, portID, channelID)
}

// OnChanCloseConfirm implements the IBCModule interface.
func (m FeeAddressIBCMiddleware) OnChanCloseConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	return m.app.OnChanCloseConfirm(ctx, portID, channelID)
}

// OnRecvPacket implements the IBCModule interface.
// It validates that only utia can be sent to the fee address via IBC.
// Non-utia transfers to the fee address are rejected with an error acknowledgement,
// causing the source chain to refund the sender.
func (m FeeAddressIBCMiddleware) OnRecvPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) ibcexported.Acknowledgement {
	var data transfertypes.FungibleTokenPacketData
	if err := transfertypes.ModuleCdc.UnmarshalJSON(packet.GetData(), &data); err != nil {
		// Not a fungible token packet, pass through to wrapped module
		return m.app.OnRecvPacket(ctx, packet, relayer)
	}

	// Check if receiver is the fee address
	if data.Receiver == feeaddresstypes.FeeAddressBech32 {
		// Parse the denom to get the base denom.
		// For returning utia, the denom is "transfer/channel-X/utia" (prefixed).
		// For foreign tokens, the base denom is not utia.
		denomTrace := transfertypes.ParseDenomTrace(data.Denom)
		baseDenom := denomTrace.GetBaseDenom()

		// Only allow utia (native or returning) to be sent to fee address.
		if baseDenom != appconsts.BondDenom {
			return channeltypes.NewErrorAcknowledgement(
				fmt.Errorf("only %s can be sent to fee address via IBC, got %s (base denom: %s)",
					appconsts.BondDenom, data.Denom, baseDenom),
			)
		}
	}

	return m.app.OnRecvPacket(ctx, packet, relayer)
}

// OnAcknowledgementPacket implements the IBCModule interface.
func (m FeeAddressIBCMiddleware) OnAcknowledgementPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	acknowledgement []byte,
	relayer sdk.AccAddress,
) error {
	return m.app.OnAcknowledgementPacket(ctx, packet, acknowledgement, relayer)
}

// OnTimeoutPacket implements the IBCModule interface.
func (m FeeAddressIBCMiddleware) OnTimeoutPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) error {
	return m.app.OnTimeoutPacket(ctx, packet, relayer)
}
