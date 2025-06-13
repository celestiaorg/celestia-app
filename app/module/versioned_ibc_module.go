package module

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	channeltypes "github.com/cosmos/ibc-go/v6/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v6/modules/core/05-port/types"
	exported "github.com/cosmos/ibc-go/v6/modules/core/exported"
)

func NewVersionedIBCModule(
	wrappedModule, nextModule porttypes.IBCModule,
	fromVersion, toVersion uint64,
) porttypes.IBCModule {
	return &VersionedIBCModule{
		wrappedModule: wrappedModule,
		nextModule:    nextModule,
		fromVersion:   fromVersion,
		toVersion:     toVersion,
	}
}

var _ porttypes.IBCModule = (*VersionedIBCModule)(nil)

type VersionedIBCModule struct {
	wrappedModule, nextModule porttypes.IBCModule
	fromVersion, toVersion    uint64
}

func (v *VersionedIBCModule) OnChanOpenInit(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID string,
	channelID string,
	channelCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	version string,
) (string, error) {
	if v.isVersionSupported(ctx) {
		return v.wrappedModule.OnChanOpenInit(ctx, order, connectionHops, portID, channelID, channelCap, counterparty, version)
	}
	return v.nextModule.OnChanOpenInit(ctx, order, connectionHops, portID, channelID, channelCap, counterparty, version)
}

func (v *VersionedIBCModule) OnChanOpenTry(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID,
	channelID string,
	channelCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	counterpartyVersion string,
) (version string, err error) {
	if v.isVersionSupported(ctx) {
		return v.wrappedModule.OnChanOpenTry(ctx, order, connectionHops, portID, channelID, channelCap, counterparty, counterpartyVersion)
	}
	return v.nextModule.OnChanOpenTry(ctx, order, connectionHops, portID, channelID, channelCap, counterparty, counterpartyVersion)
}

func (v *VersionedIBCModule) OnChanOpenAck(
	ctx sdk.Context,
	portID,
	channelID string,
	counterpartyChannelID string,
	counterpartyVersion string,
) error {
	if v.isVersionSupported(ctx) {
		return v.wrappedModule.OnChanOpenAck(ctx, portID, channelID, counterpartyChannelID, counterpartyVersion)
	}
	return v.nextModule.OnChanOpenAck(ctx, portID, channelID, counterpartyChannelID, counterpartyVersion)
}

func (v *VersionedIBCModule) OnChanOpenConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	if v.isVersionSupported(ctx) {
		return v.wrappedModule.OnChanOpenConfirm(ctx, portID, channelID)
	}
	return v.nextModule.OnChanOpenConfirm(ctx, portID, channelID)
}

func (v *VersionedIBCModule) OnChanCloseInit(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	if v.isVersionSupported(ctx) {
		return v.wrappedModule.OnChanCloseInit(ctx, portID, channelID)
	}
	return v.nextModule.OnChanCloseInit(ctx, portID, channelID)
}

func (v *VersionedIBCModule) OnChanCloseConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	if v.isVersionSupported(ctx) {
		return v.wrappedModule.OnChanCloseConfirm(ctx, portID, channelID)
	}
	return v.nextModule.OnChanCloseConfirm(ctx, portID, channelID)
}

func (v *VersionedIBCModule) OnRecvPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) exported.Acknowledgement {
	if v.isVersionSupported(ctx) {
		return v.wrappedModule.OnRecvPacket(ctx, packet, relayer)
	}
	return v.nextModule.OnRecvPacket(ctx, packet, relayer)
}

func (v *VersionedIBCModule) OnAcknowledgementPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	acknowledgement []byte,
	relayer sdk.AccAddress,
) error {
	if v.isVersionSupported(ctx) {
		return v.wrappedModule.OnAcknowledgementPacket(ctx, packet, acknowledgement, relayer)
	}
	return v.nextModule.OnAcknowledgementPacket(ctx, packet, acknowledgement, relayer)
}

func (v *VersionedIBCModule) OnTimeoutPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) error {
	if v.isVersionSupported(ctx) {
		return v.wrappedModule.OnTimeoutPacket(ctx, packet, relayer)
	}
	return v.nextModule.OnTimeoutPacket(ctx, packet, relayer)
}

func (v *VersionedIBCModule) isVersionSupported(ctx sdk.Context) bool {
	currentAppVersion := ctx.BlockHeader().Version.App
	return currentAppVersion >= v.fromVersion && currentAppVersion <= v.toVersion
}
