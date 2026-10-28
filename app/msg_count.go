package app

import (
	"encoding/json"

	storetypes "cosmossdk.io/store/types"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	zkismtypes "github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
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

// txGasLimit returns the gas limit tx declares, which caps the gas its
// execution can consume. ok is false if tx declares none; ante rejects it.
func txGasLimit(tx sdk.Tx) (gas uint64, ok bool) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return 0, false
	}
	return feeTx.GetGas(), true
}

// fitsSDKGas reports whether a tx with gas limit txGas fits a block whose txs
// already declare used gas, with used <= MaxSDKGas. Subtracting rather than
// adding keeps an attacker chosen txGas from overflowing the sum.
func fitsSDKGas(used, txGas uint64) bool {
	return txGas <= appconsts.MaxSDKGas-used
}

// countZKProofMsgs counts messages that perform ZK proof verification or
// verifying-key pairing work, including messages dispatched by authz and ICA wrappers.
func countZKProofMsgs(ctx sdk.Context, ck channelKeeper, msgs []sdk.Msg) int {
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	count := 0
	for _, msg := range msgs {
		count += zkProofMsgWeight(ctx, ck, msg)
		if count > appconsts.MaxZKProofMessages {
			return count
		}
	}
	return count
}

func zkProofMsgWeight(ctx sdk.Context, ck channelKeeper, msg sdk.Msg) int {
	switch msg := msg.(type) {
	case *zkismtypes.MsgCreateInterchainSecurityModule, *zkismtypes.MsgUpdateInterchainSecurityModule, *zkismtypes.MsgSubmitMessages, *zkismtypes.MsgUpdateVerifyingKeys:
		return 1
	case *authz.MsgExec:
		return execWeight(ctx, ck, msg, zkProofMsgWeight)
	case *channeltypes.MsgRecvPacket:
		return countICAPacketZKProofMsgs(ctx, ck, msg.Packet)
	default:
		return 0
	}
}

// countISMSignatures counts the signatures Hyperlane MsgProcessMessage metadata
// carries, including through authz and ICA wrappers.
func countISMSignatures(ctx sdk.Context, ck channelKeeper, msgs []sdk.Msg) int {
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	count := 0
	for _, msg := range msgs {
		count += ismSignatureWeight(ctx, ck, msg)
		if count > appconsts.MaxISMSignatures {
			return count
		}
	}
	return count
}

func ismSignatureWeight(ctx sdk.Context, ck channelKeeper, msg sdk.Msg) int {
	switch msg := msg.(type) {
	case *hyperlanetypes.MsgProcessMessage:
		return ante.ProcessMessageSignatures(msg.Metadata)
	case *authz.MsgExec:
		return execWeight(ctx, ck, msg, ismSignatureWeight)
	case *channeltypes.MsgRecvPacket:
		return countICAPacketISMSignatures(ctx, ck, msg.Packet)
	default:
		return 0
	}
}

// execWeight sums weigh over the messages a MsgExec dispatches, skipping a
// nested MsgExec, which ante rejects, rather than recursing into it.
func execWeight(ctx sdk.Context, ck channelKeeper, exec *authz.MsgExec, weigh func(sdk.Context, channelKeeper, sdk.Msg) int) int {
	nested, err := exec.GetMessages()
	if err != nil {
		return 0
	}
	count := 0
	for _, nestedMsg := range nested {
		if _, nestedExec := nestedMsg.(*authz.MsgExec); nestedExec {
			continue
		}
		count += weigh(ctx, ck, nestedMsg)
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

// countICAPacketZKProofMsgs returns how many ZK proof messages the ICA host
// would dispatch. Malformed host payloads count over the limit because ante
// validation occurs after proposal counting.
func countICAPacketZKProofMsgs(ctx sdk.Context, ck channelKeeper, packet channeltypes.Packet) int {
	return countICAPacketWeighted(ctx, ck, packet, zkProofTypeURLWeight, appconsts.MaxZKProofMessages+1)
}

// countICAPacketISMSignatures fails closed on a MsgProcessMessage in an ICA
// payload: the wire-level walk sees type URLs, not metadata lengths.
func countICAPacketISMSignatures(ctx sdk.Context, ck channelKeeper, packet channeltypes.Packet) int {
	return countICAPacketWeighted(ctx, ck, packet, ismTypeURLWeight, appconsts.MaxISMSignatures+1)
}

// countICAPacketWeighted sums weigh over the type URLs the ICA host would
// dispatch from packet, or returns over if the payload cannot be walked.
func countICAPacketWeighted(ctx sdk.Context, ck channelKeeper, packet channeltypes.Packet, weigh func(typeURL string) int, over int) int {
	if packet.DestinationPort != icatypes.HostPortID {
		return 0
	}

	var data icatypes.InterchainAccountPacketData
	if err := data.UnmarshalJSON(packet.Data); err != nil || data.Type != icatypes.EXECUTE_TX {
		return 0
	}
	if err := host.ChannelIdentifierValidator(packet.DestinationChannel); err != nil {
		return over
	}
	version, ok := ck.GetAppVersion(ctx, packet.DestinationPort, packet.DestinationChannel)
	if !ok {
		return over
	}
	metadata, err := icatypes.MetadataFromVersion(version)
	if err != nil {
		return over
	}

	count := -1
	switch metadata.Encoding {
	case icatypes.EncodingProtobuf:
		count = countProtoWeighted(data.Data, weigh, over)
	case icatypes.EncodingProto3JSON:
		count = countJSONWeighted(data.Data, weigh, over)
	}
	if count < 0 {
		return over
	}
	return count
}

func zkProofTypeURLWeight(typeURL string) int {
	if isZKProofTypeURL(typeURL) {
		return 1
	}
	return 0
}

// ismTypeURLWeight counts a MsgProcessMessage as over budget; see above.
func ismTypeURLWeight(typeURL string) int {
	if typeURL == sdk.MsgTypeURL(&hyperlanetypes.MsgProcessMessage{}) {
		return appconsts.MaxISMSignatures + 1
	}
	return 0
}

// countProtoWeighted sums weigh over a proto3 CosmosTx, or -1 if bz is not one.
func countProtoWeighted(bz []byte, weigh func(string) int, over int) int {
	count := 0
	for len(bz) > 0 {
		num, typ, n := protowire.ConsumeTag(bz)
		if n < 0 {
			return -1
		}
		bz = bz[n:]
		if typ == protowire.StartGroupType || typ == protowire.EndGroupType {
			return -1
		}
		if num == cosmosTxMsgsField && typ == protowire.BytesType {
			anyBz, consumed := protowire.ConsumeBytes(bz)
			if consumed < 0 {
				return -1
			}
			bz = bz[consumed:]
			typeURL, ok := protoAnyTypeURL(anyBz)
			if !ok {
				return -1
			}
			if isFanOutTypeURL(typeURL) {
				return over
			}
			count += weigh(typeURL)
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, bz)
		if n < 0 {
			return -1
		}
		bz = bz[n:]
	}
	return count
}

func protoAnyTypeURL(bz []byte) (string, bool) {
	var typeURL string
	for len(bz) > 0 {
		num, typ, n := protowire.ConsumeTag(bz)
		if n < 0 {
			return "", false
		}
		bz = bz[n:]
		if typ == protowire.StartGroupType || typ == protowire.EndGroupType {
			return "", false
		}
		if num == 1 && typ == protowire.BytesType {
			value, consumed := protowire.ConsumeBytes(bz)
			if consumed < 0 {
				return "", false
			}
			typeURL = string(value)
			bz = bz[consumed:]
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, bz)
		if n < 0 {
			return "", false
		}
		bz = bz[n:]
	}
	return typeURL, typeURL != ""
}

// isFanOutTypeURL reports whether a message dispatches further messages, which
// the wire-level ICA counter cannot see into. None are on the default ICA allow
// list, so fail closed if governance allows one rather than letting inner proof
// work bypass the counter. See TestIcaAllowMessagesExcludeFanOutMsgs.
func isFanOutTypeURL(typeURL string) bool {
	return typeURL == sdk.MsgTypeURL(&authz.MsgExec{}) ||
		typeURL == sdk.MsgTypeURL(&govv1.MsgSubmitProposal{}) ||
		typeURL == sdk.MsgTypeURL(&channeltypes.MsgRecvPacket{})
}

func isZKProofTypeURL(typeURL string) bool {
	return typeURL == sdk.MsgTypeURL(&zkismtypes.MsgCreateInterchainSecurityModule{}) ||
		typeURL == sdk.MsgTypeURL(&zkismtypes.MsgUpdateInterchainSecurityModule{}) ||
		typeURL == sdk.MsgTypeURL(&zkismtypes.MsgSubmitMessages{}) ||
		typeURL == sdk.MsgTypeURL(&zkismtypes.MsgUpdateVerifyingKeys{})
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

// countJSONWeighted sums weigh over a proto3json CosmosTx, or -1 if bz is not one.
func countJSONWeighted(bz []byte, weigh func(string) int, over int) int {
	var tx struct {
		Messages []struct {
			TypeURL string `json:"@type"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(bz, &tx); err != nil {
		return -1
	}
	count := 0
	for _, msg := range tx.Messages {
		if msg.TypeURL == "" {
			return -1
		}
		if isFanOutTypeURL(msg.TypeURL) {
			return over
		}
		count += weigh(msg.TypeURL)
	}
	return count
}
