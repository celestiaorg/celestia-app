package app

import (
	"encoding/json"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
	"google.golang.org/protobuf/encoding/protowire"
)

// cosmosTxMsgsField is the field number of CosmosTx.messages.
const cosmosTxMsgsField = 1

// channelKeeper reads the version an ICA channel negotiated, which says how its
// packet payloads are encoded.
type channelKeeper interface {
	GetAppVersion(ctx sdk.Context, portID, channelID string) (string, bool)
}

// countExecutableMsgs counts messages the way a block executes them: every
// message costs one, plus whatever it dispatches. This stops a tx from hiding
// many executable messages behind a single wrapper to bypass the per-block SDK
// message limit.
func countExecutableMsgs(ctx sdk.Context, ck channelKeeper, msgs []sdk.Msg) int {
	// Counting filters a proposal, it does not execute the tx, so reading the
	// channel must not draw on the gas the previous tx left on the meter.
	// Otherwise an unrelated tx with a tight gas limit ahead of this one makes
	// the read run out of gas and panic.
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())

	count := 0
	for _, msg := range msgs {
		count += msgWeight(ctx, ck, msg)
		// Callers only compare the count against the limit, so stop once it is
		// past it rather than reading a channel for every remaining packet.
		if count > appconsts.MaxSDKMessages {
			return count
		}
	}
	return count
}

// msgWeight returns what a message costs: one for the message itself, plus the
// messages it dispatches. A message with no fan-out, or an empty one, still
// weighs one, since it costs a full ante pass either way.
func msgWeight(ctx sdk.Context, ck channelKeeper, msg sdk.Msg) int {
	if exec, ok := msg.(*authz.MsgExec); ok {
		return 1 + execFanout(ctx, ck, exec)
	}
	return leafWeight(ctx, ck, msg)
}

// leafWeight is msgWeight for a message that is not a MsgExec.
func leafWeight(ctx sdk.Context, ck channelKeeper, msg sdk.Msg) int {
	switch msg := msg.(type) {
	case *icahosttypes.MsgModuleQuerySafe:
		return 1 + len(msg.Requests)
	case *channeltypes.MsgRecvPacket:
		return 1 + countICAPacketMsgs(ctx, ck, msg.Packet)
	}
	return 1
}

// execFanout counts the messages a MsgExec dispatches, not counting the MsgExec
// itself. It does not recurse: the ante handler rejects a nested MsgExec, and
// counting runs before the ante handler, so recursing on attacker controlled
// nesting could exhaust the stack in PrepareProposal, which has no recover.
func execFanout(ctx sdk.Context, ck channelKeeper, exec *authz.MsgExec) int {
	nested, err := exec.GetMessages()
	if err != nil {
		// The ante handler rejects a tx with undecodable inner messages, so
		// count the wrappers rather than treating them as free.
		return len(exec.Msgs)
	}
	count := 0
	for _, msg := range nested {
		count += leafWeight(ctx, ck, msg)
	}
	return count
}

// countICAPacketMsgs returns how many messages the ICA host would dispatch from
// packet: zero if the packet is not bound for the host or does not decode, or
// MaxSDKMessages if the payload cannot be counted under the channel's encoding.
func countICAPacketMsgs(ctx sdk.Context, ck channelKeeper, packet channeltypes.Packet) int {
	if packet.DestinationPort != icatypes.HostPortID {
		return 0
	}

	var data icatypes.InterchainAccountPacketData
	if err := data.UnmarshalJSON(packet.Data); err != nil {
		return 0
	}
	if data.Type != icatypes.EXECUTE_TX {
		return 0
	}

	// Bound the channel identifier before it reaches the store, which panics on
	// an oversized key. The packet is attacker controlled and MsgRecvPacket's
	// ValidateBasic only runs later, in the ante handler.
	if err := host.ChannelIdentifierValidator(packet.DestinationChannel); err != nil {
		return appconsts.MaxSDKMessages
	}

	// Count with the encoding the channel negotiated, the same way the host
	// picks its decoder.
	version, ok := ck.GetAppVersion(ctx, packet.DestinationPort, packet.DestinationChannel)
	if !ok {
		return appconsts.MaxSDKMessages
	}
	metadata, err := icatypes.MetadataFromVersion(version)
	if err != nil {
		return appconsts.MaxSDKMessages
	}

	// Count the message list without materialising the messages in it. Counting
	// runs before the ante handler charges any gas, so an 8 MiB payload of
	// minimal messages would otherwise cost every node hundreds of MB for free.
	count := -1
	switch metadata.Encoding {
	case icatypes.EncodingProtobuf:
		count = countProtoMsgs(data.Data)
	case icatypes.EncodingProto3JSON:
		count = countJSONMsgs(data.Data)
	}
	if count < 0 {
		// This is not the host's decoder, so bytes it rejects may still dispatch
		// messages there. Counting such a payload as free would bypass the
		// limit, so count it as over the limit instead.
		return appconsts.MaxSDKMessages
	}
	return count
}

// countProtoMsgs counts the entries of a proto3 encoded CosmosTx, or -1 if the
// bytes are not one. It walks the wire format instead of decoding, so the cost
// does not depend on how many entries there are.
func countProtoMsgs(bz []byte) int {
	count := 0
	for len(bz) > 0 {
		num, typ, n := protowire.ConsumeTag(bz)
		if n < 0 {
			return -1
		}
		bz = bz[n:]
		// Groups would make ConsumeFieldValue recurse as deep as the input is
		// long. proto3 has no groups, so reject them rather than walk them.
		if typ == protowire.StartGroupType || typ == protowire.EndGroupType {
			return -1
		}
		if n = protowire.ConsumeFieldValue(num, typ, bz); n < 0 {
			return -1
		}
		bz = bz[n:]
		if num == cosmosTxMsgsField && typ == protowire.BytesType {
			count++
		}
	}
	return count
}

// countJSONMsgs counts the entries of a proto3json encoded CosmosTx, or -1 if
// the bytes are not one. Entries decode into an empty struct, so only the list
// length is retained.
func countJSONMsgs(bz []byte) int {
	var tx struct {
		Messages []struct{} `json:"messages"`
	}
	if err := json.Unmarshal(bz, &tx); err != nil {
		return -1
	}
	return len(tx.Messages)
}
