package app

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	burntypes "github.com/celestiaorg/celestia-app/v7/x/burn/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
)

var _ porttypes.IBCModule = (*BurnAddressIBCMiddleware)(nil)

// BurnAddressIBCMiddleware rejects inbound IBC transfers of non-utia tokens
// to the burn address. This prevents foreign tokens from getting permanently
// stuck at the burn address since the EndBlocker only burns utia.
//
// When a transfer is rejected, an error acknowledgement is returned, causing
// the source chain to refund the sender.
type BurnAddressIBCMiddleware struct {
	app porttypes.IBCModule
}

// NewBurnAddressIBCMiddleware creates a new BurnAddressIBCMiddleware.
func NewBurnAddressIBCMiddleware(app porttypes.IBCModule) BurnAddressIBCMiddleware {
	return BurnAddressIBCMiddleware{app: app}
}

// OnChanOpenInit implements the IBCModule interface.
func (m BurnAddressIBCMiddleware) OnChanOpenInit(
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
func (m BurnAddressIBCMiddleware) OnChanOpenTry(
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
func (m BurnAddressIBCMiddleware) OnChanOpenAck(
	ctx sdk.Context,
	portID,
	channelID string,
	counterpartyChannelID string,
	counterpartyVersion string,
) error {
	return m.app.OnChanOpenAck(ctx, portID, channelID, counterpartyChannelID, counterpartyVersion)
}

// OnChanOpenConfirm implements the IBCModule interface.
func (m BurnAddressIBCMiddleware) OnChanOpenConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	return m.app.OnChanOpenConfirm(ctx, portID, channelID)
}

// OnChanCloseInit implements the IBCModule interface.
func (m BurnAddressIBCMiddleware) OnChanCloseInit(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	return m.app.OnChanCloseInit(ctx, portID, channelID)
}

// OnChanCloseConfirm implements the IBCModule interface.
func (m BurnAddressIBCMiddleware) OnChanCloseConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	return m.app.OnChanCloseConfirm(ctx, portID, channelID)
}

// OnRecvPacket implements the IBCModule interface.
// It validates that only utia can be sent to the burn address via IBC.
// Non-utia transfers to the burn address are rejected with an error acknowledgement,
// causing the source chain to refund the sender.
func (m BurnAddressIBCMiddleware) OnRecvPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) ibcexported.Acknowledgement {
	var data transfertypes.FungibleTokenPacketData
	if err := transfertypes.ModuleCdc.UnmarshalJSON(packet.GetData(), &data); err != nil {
		// Not a fungible token packet, pass through to wrapped module
		return m.app.OnRecvPacket(ctx, packet, relayer)
	}

	// Check if receiver is the burn address
	if data.Receiver == burntypes.BurnAddressBech32 {
		// Parse the denom to get the base denom.
		// For returning utia, the denom is "transfer/channel-X/utia" (prefixed).
		// For foreign tokens, the base denom is not utia.
		denomTrace := transfertypes.ParseDenomTrace(data.Denom)
		baseDenom := denomTrace.GetBaseDenom()

		// Only allow utia (native or returning) to be sent to burn address.
		if baseDenom != appconsts.BondDenom {
			return channeltypes.NewErrorAcknowledgement(
				fmt.Errorf("only %s can be sent to burn address via IBC, got %s (base denom: %s)",
					appconsts.BondDenom, data.Denom, baseDenom),
			)
		}
	}

	return m.app.OnRecvPacket(ctx, packet, relayer)
}

// OnAcknowledgementPacket implements the IBCModule interface.
func (m BurnAddressIBCMiddleware) OnAcknowledgementPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	acknowledgement []byte,
	relayer sdk.AccAddress,
) error {
	return m.app.OnAcknowledgementPacket(ctx, packet, acknowledgement, relayer)
}

// OnTimeoutPacket implements the IBCModule interface.
func (m BurnAddressIBCMiddleware) OnTimeoutPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) error {
	return m.app.OnTimeoutPacket(ctx, packet, relayer)
}
