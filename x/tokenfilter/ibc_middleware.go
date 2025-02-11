package tokenfilter

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"
)

const ModuleName = "tokenfilter"

// tokenFilterMiddleware directly inherits the IBCModule and ICS4Wrapper interfaces.
// Only with OnRecvPacket, does it wrap the underlying implementation with additional
// stateless logic for rejecting the inbound transfer of non-native tokens. This
// middleware is unilateral and no handshake is required. If using this middleware
// on an existing chain, tokens that have been routed through this chain will still
// be allowed to unwrap.
type tokenFilterMiddleware struct {
	porttypes.IBCModule
}

// NewIBCMiddleware creates a new instance of the token filter middleware for
// the transfer module.
func NewIBCMiddleware(ibcModule porttypes.IBCModule) porttypes.IBCModule {
	return &tokenFilterMiddleware{
		IBCModule: ibcModule,
	}
}

// OnRecvPacket implements the IBCModule interface. It is called whenever a new packet
// from another chain is received on this chain. Here, the token filter middleware
// unmarshals the FungibleTokenPacketData and checks to see if the denomination being
// transferred to this chain originally came from this chain i.e. is a native token.
// If not, it returns an ErrorAcknowledgement.
func (m *tokenFilterMiddleware) OnRecvPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) exported.Acknowledgement {
	var data transfertypes.FungibleTokenPacketData
	if err := transfertypes.ModuleCdc.UnmarshalJSON(packet.GetData(), &data); err != nil {
		// If this happens either a) a user has crafted an invalid packet, b) a
		// software developer has connected the middleware to a stack that does
		// not have a transfer module, or c) the transfer module has been modified
		// to accept other Packets. The best thing we can do here is pass the packet
		// on down the stack.
		return m.IBCModule.OnRecvPacket(ctx, packet, relayer)
	}

	// This checks the first channel and port in the denomination path. If it matches
	// our channel and port it means that the token was originally sent from this
	// chain. Note that this firewall prevents routing of other transactions through
	// the chain so from this logic, the denom has to be a native denom.
	if transfertypes.ReceiverChainIsSource(packet.GetSourcePort(), packet.GetSourceChannel(), data.Denom) {
		return m.IBCModule.OnRecvPacket(ctx, packet, relayer)
	}

	ackErr := errors.Wrapf(sdkerrors.ErrInvalidType, "only native denom transfers accepted, got %s", data.Denom)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			transfertypes.EventTypePacket,
			sdk.NewAttribute(sdk.AttributeKeyModule, ModuleName),
			sdk.NewAttribute(sdk.AttributeKeySender, data.Sender),
			sdk.NewAttribute(transfertypes.AttributeKeyReceiver, data.Receiver),
			sdk.NewAttribute(transfertypes.AttributeKeyDenom, data.Denom),
			sdk.NewAttribute(transfertypes.AttributeKeyAmount, data.Amount),
			sdk.NewAttribute(transfertypes.AttributeKeyMemo, data.Memo),
			sdk.NewAttribute(transfertypes.AttributeKeyAckSuccess, "false"),
			sdk.NewAttribute(transfertypes.AttributeKeyAckError, ackErr.Error()),
		),
	)

	return channeltypes.NewErrorAcknowledgement(ackErr)
}
