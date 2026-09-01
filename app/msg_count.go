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
	"google.golang.org/protobuf/encoding/protowire"
)

// cosmosTxMsgsField is the field number of CosmosTx.messages.
const cosmosTxMsgsField = 1

// channelKeeper reads the version an ICA channel negotiated, which says how its
// packet payloads are encoded.
type channelKeeper interface {
	GetAppVersion(ctx sdk.Context, portID, channelID string) (string, bool)
}

// countExecutableMsgs counts messages the way a block executes them. A MsgExec
// contributes the messages authz dispatches from it, a MsgModuleQuerySafe
// contributes the queries it dispatches, and a MsgRecvPacket bound for the ICA
// host contributes the messages the host dispatches from its payload. This stops
// a tx from hiding many executable messages behind a single wrapper to bypass the
// per-block SDK message limit. Nested MsgExec is rejected by the ante handler, so
// counting one level is enough.
func countExecutableMsgs(ctx sdk.Context, ck channelKeeper, msgs []sdk.Msg) int {
	// Counting filters a proposal, it does not execute the tx, so reading the
	// channel must not draw on the gas the previous tx left on the meter.
	// Otherwise an unrelated tx with a tight gas limit ahead of this one makes
	// the read run out of gas and panic.
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())

	count := 0
	for _, msg := range msgs {
		switch msg := msg.(type) {
		case *authz.MsgExec:
			count += countExecMsgs(ctx, ck, msg)
		case *channeltypes.MsgRecvPacket:
			count += 1 + countICAPacketMsgs(ctx, ck, msg.Packet)
		default:
			count += msgWeight(msg)
		}
	}
	return count
}

// msgWeight returns the number of executable units in a single message that does
// not expand further. It never returns less than one: a message with an empty
// fan-out still costs a full ante pass, and per-message ValidateBasic does not
// run in the ante chain that ProcessProposal uses.
func msgWeight(msg sdk.Msg) int {
	if querySafe, ok := msg.(*icahosttypes.MsgModuleQuerySafe); ok {
		return max(1, len(querySafe.Requests))
	}
	return 1
}

// countExecMsgs counts the messages a MsgExec dispatches. A wrapped
// MsgRecvPacket expands further, so it is counted like a top level one.
func countExecMsgs(ctx sdk.Context, ck channelKeeper, exec *authz.MsgExec) int {
	nested, err := exec.GetMessages()
	if err != nil {
		// The ante handler rejects a tx with undecodable inner messages, so
		// count the wrappers rather than treating the MsgExec as free.
		return len(exec.Msgs)
	}
	count := 0
	for _, msg := range nested {
		if recv, ok := msg.(*channeltypes.MsgRecvPacket); ok {
			count += 1 + countICAPacketMsgs(ctx, ck, recv.Packet)
			continue
		}
		count += msgWeight(msg)
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
