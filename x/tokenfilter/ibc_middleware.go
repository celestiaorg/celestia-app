package tokenfilter

import (
	"encoding/json"
	"strconv"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	transfertypes "github.com/cosmos/ibc-go/v9/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v9/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v9/modules/core/05-port/types"
	"github.com/cosmos/ibc-go/v9/modules/core/exported"
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
	channelVersion string,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) exported.Acknowledgement {
	data, err := transfertypes.UnmarshalPacketData(packet.GetData(), channelVersion)
	if err != nil {
		// If this happens either a) a user has crafted an invalid packet, b) a
		// software developer has connected the middleware to a stack that does
		// not have a transfer module, or c) the transfer module has been modified
		// to accept other Packets. The best thing we can do here is pass the packet
		// on down the stack.
		return m.IBCModule.OnRecvPacket(ctx, channelVersion, packet, relayer)
	}

	if len(data.Tokens) != 1 {
		ackErr := errors.Wrapf(sdkerrors.ErrInvalidType, "expected exactly one token, got %d", len(data.Tokens))
		return createErrorAcknowledgement(ctx, data, ackErr)
	}

	// Note, this firewall prevents receiving of any non-native token denominations as we only
	// accept ibc tokens which were minted on behalf of this chain.
	denom := data.Tokens[0].Denom
	if receiverChainIsSource(packet.GetSourcePort(), packet.GetSourceChannel(), denom) {
		return m.IBCModule.OnRecvPacket(ctx, channelVersion, packet, relayer)
	}

	ackErr := errors.Wrapf(sdkerrors.ErrInvalidType, "only native denom transfers accepted, got %s", denom.Path())
	return createErrorAcknowledgement(ctx, data, ackErr)
}

// receiverChainIsSource checks the first denomination prefix in the ibc transfer denom provided against the packet source port and channel.
// If the prefix matches it means that the token was originally sent from this chain.
// This is because ibc transfer prefixes the destination port and channel to the token denomination when minting vouchers in recv packet.
func receiverChainIsSource(srcPort, srcChannel string, denom transfertypes.Denom) bool {
	return denom.HasPrefix(srcPort, srcChannel)
}

// createErrorAcknowledgement emits an ibc transfer event and creates an ibc error acknowledgement.
func createErrorAcknowledgement(ctx sdk.Context, data transfertypes.FungibleTokenPacketDataV2, ackErr error) exported.Acknowledgement {
	ack := channeltypes.NewErrorAcknowledgement(ackErr)
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			transfertypes.EventTypePacket,
			sdk.NewAttribute(sdk.AttributeKeyModule, ModuleName),
			sdk.NewAttribute(sdk.AttributeKeySender, data.Sender),
			sdk.NewAttribute(transfertypes.AttributeKeyReceiver, data.Receiver),
			sdk.NewAttribute(transfertypes.AttributeKeyMemo, data.Memo),
			sdk.NewAttribute(transfertypes.AttributeKeyTokens, mustMarshalJSON(data.Tokens)),
			sdk.NewAttribute(transfertypes.AttributeKeyForwardingHops, mustMarshalJSON(data.Forwarding)),
			sdk.NewAttribute(transfertypes.AttributeKeyMemo, data.Memo),
			sdk.NewAttribute(transfertypes.AttributeKeyAckSuccess, strconv.FormatBool(ack.Success())),
			sdk.NewAttribute(transfertypes.AttributeKeyAckError, ackErr.Error()),
		),
	)

	return ack
}

func mustMarshalJSON(v any) string {
	bz, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bz)
}
