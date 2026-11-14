package app

import (
	"strings"

	apperrors "github.com/celestiaorg/celestia-app/v11/app/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
)

const (
	// pfmErrorAckPrefix starts the text of every error acknowledgement that
	// packet-forward-middleware creates.
	pfmErrorAckPrefix = "packet-forward-middleware error: "
)

// pfmIBCMiddleware wraps packet-forward-middleware so that the error
// acknowledgements it returns from OnRecvPacket contain no Go error text.
// ibc-go commits the hash of the acknowledgement, so its bytes are part of
// consensus.
type pfmIBCMiddleware struct {
	packetforward.IBCMiddleware
}

// OnRecvPacket implements porttypes.IBCModule.
func (m pfmIBCMiddleware) OnRecvPacket(ctx sdk.Context, packet channeltypes.Packet, relayer sdk.AccAddress) ibcexported.Acknowledgement {
	// Reject, before packet-forward-middleware runs, a forward memo with a
	// numeric timeout outside the int64 range (its conversion to
	// time.Duration depends on the CPU architecture) or with no forward
	// metadata after decoding (packet-forward-middleware does not handle it),
	// or a forward memo outside the limits of pfmForwardMemoWithinLimits
	// (too long, too deep or with a repeated key, which would make
	// packet-forward-middleware do work that grows faster than the memo).
	if rejectPFMForwardMemo(packet.GetData()) {
		ctx.Logger().Info("rejected packet-forward-middleware forward memo")
		return channeltypes.NewErrorAcknowledgement(apperrors.ErrPacketForward)
	}
	ack := m.IBCMiddleware.OnRecvPacket(ctx, packet, relayer)
	if fixed, ok := fixedPFMErrorAck(ctx, ack); ok {
		return fixed
	}
	return ack
}

// pfmICS4Wrapper wraps the ICS4Wrapper that packet-forward-middleware uses
// to write the acknowledgements of forwarded packets (after the ack or the
// timeout of the forwarded packet). It replaces packet-forward-middleware
// error acknowledgements with the fixed one before they are committed.
type pfmICS4Wrapper struct {
	porttypes.ICS4Wrapper
}

// WriteAcknowledgement implements porttypes.ICS4Wrapper.
func (w pfmICS4Wrapper) WriteAcknowledgement(ctx sdk.Context, chanCap *capabilitytypes.Capability, packet ibcexported.PacketI, ack ibcexported.Acknowledgement) error {
	if fixed, ok := fixedPFMErrorAck(ctx, ack); ok {
		ack = fixed
	}
	return w.ICS4Wrapper.WriteAcknowledgement(ctx, chanCap, packet, ack)
}

// fixedPFMErrorAck returns the fixed error acknowledgement when ack is a
// packet-forward-middleware error acknowledgement. The fixed one carries only
// the ABCI code of apperrors.ErrPacketForward. The original text goes to the
// node log.
func fixedPFMErrorAck(ctx sdk.Context, ack ibcexported.Acknowledgement) (channeltypes.Acknowledgement, bool) {
	a, ok := ack.(channeltypes.Acknowledgement)
	if !ok {
		return channeltypes.Acknowledgement{}, false
	}
	e, ok := a.Response.(*channeltypes.Acknowledgement_Error)
	if !ok || !strings.HasPrefix(e.Error, pfmErrorAckPrefix) {
		return channeltypes.Acknowledgement{}, false
	}
	// The text goes to the node log only. An event would put it in the ABCI
	// results, where nodes from different builds would disagree on it.
	ctx.Logger().Info("replaced packet-forward-middleware error acknowledgement", "error", e.Error)
	return channeltypes.NewErrorAcknowledgement(apperrors.ErrPacketForward), true
}
