package app

import (
	"bytes"
	"encoding/json"

	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	zkismtypes "github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
)

// cosmosTxMsgsField is the field number of CosmosTx.messages.
const cosmosTxMsgsField = 1

// countExecutableMsgs counts messages the way a block executes them: every
// message costs one, plus whatever it dispatches. This stops a tx from hiding
// many executable messages behind a single wrapper to bypass the per-block SDK
// message limit. It reads no state, so it gives the same count in CheckTx,
// PrepareProposal, ProcessProposal and the ante handler, whatever earlier txs
// in the block change.
func countExecutableMsgs(msgs []sdk.Msg) int {
	count := 0
	for _, msg := range msgs {
		count += msgWeight(msg)
		// Callers only compare the count against the limit, so stop once it is
		// past it rather than walking every remaining packet.
		if count > appconsts.MaxSDKMessages {
			return count
		}
	}
	return count
}

// txGasLimit returns the gas limit tx declares, which caps the gas its
// execution can consume. It reads the fee through the nil safe proto getters
// because it runs before ante, and FeeTx.GetGas panics on a tx without AuthInfo
// or Fee. A tx without a fee, or one that is not a protobuf tx, reports zero
// here and ante rejects it.
func txGasLimit(tx sdk.Tx) uint64 {
	protoTx, ok := tx.(interface{ GetProtoTx() *txtypes.Tx })
	if !ok {
		return 0
	}
	return protoTx.GetProtoTx().GetAuthInfo().GetFee().GetGasLimit()
}

// fitsSDKGas reports whether a tx with gas limit txGas fits a block whose txs
// already declare used gas, with used <= MaxSDKGas. Subtracting rather than
// adding keeps an attacker chosen txGas from overflowing the sum.
func fitsSDKGas(used, txGas uint64) bool {
	return txGas <= appconsts.MaxSDKGas-used
}

// countZKProofMsgs counts messages that perform ZK proof verification or
// verifying-key pairing work, including messages dispatched by authz and ICA wrappers.
func countZKProofMsgs(msgs []sdk.Msg) int {
	count := 0
	for _, msg := range msgs {
		count += zkProofMsgWeight(msg)
		if count > appconsts.MaxZKProofMessages {
			return count
		}
	}
	return count
}

func zkProofMsgWeight(msg sdk.Msg) int {
	switch msg := msg.(type) {
	case *zkismtypes.MsgCreateInterchainSecurityModule, *zkismtypes.MsgUpdateInterchainSecurityModule, *zkismtypes.MsgSubmitMessages, *zkismtypes.MsgUpdateVerifyingKeys:
		return 1
	case *authz.MsgExec:
		return execWeight(msg, zkProofMsgWeight)
	case *channeltypes.MsgRecvPacket:
		return countICAPacketZKProofMsgs(msg.Packet)
	default:
		return 0
	}
}

// countISMSignatures counts the signatures Hyperlane MsgProcessMessage metadata
// carries, including through authz and ICA wrappers.
func countISMSignatures(msgs []sdk.Msg) int {
	count := 0
	for _, msg := range msgs {
		count += ismSignatureWeight(msg)
		if count > appconsts.MaxISMSignatures {
			return count
		}
	}
	return count
}

func ismSignatureWeight(msg sdk.Msg) int {
	switch msg := msg.(type) {
	case *hyperlanetypes.MsgProcessMessage:
		return ante.ProcessMessageSignatures(msg.Metadata)
	case *authz.MsgExec:
		return execWeight(msg, ismSignatureWeight)
	case *channeltypes.MsgRecvPacket:
		return countICAPacketISMSignatures(msg.Packet)
	default:
		return 0
	}
}

// execWeight sums weigh over the messages a MsgExec dispatches, skipping a
// nested MsgExec, which ante rejects, rather than recursing into it.
func execWeight(exec *authz.MsgExec, weigh func(sdk.Msg) int) int {
	nested, err := exec.GetMessages()
	if err != nil {
		return 0
	}
	count := 0
	for _, nestedMsg := range nested {
		if _, nestedExec := nestedMsg.(*authz.MsgExec); nestedExec {
			continue
		}
		count += weigh(nestedMsg)
	}
	return count
}

// msgWeight returns what a message costs: one for the message itself, plus the
// messages it dispatches. A message with no fan-out, or an empty one, still
// weighs one, since it costs a full ante pass either way.
func msgWeight(msg sdk.Msg) int {
	if exec, ok := msg.(*authz.MsgExec); ok {
		return 1 + execFanout(exec)
	}
	return leafWeight(msg)
}

// leafWeight is msgWeight for a message that is not a MsgExec.
func leafWeight(msg sdk.Msg) int {
	switch msg := msg.(type) {
	case *icahosttypes.MsgModuleQuerySafe:
		return 1 + len(msg.Requests)
	case *channeltypes.MsgRecvPacket:
		return 1 + countICAPacketMsgs(msg.Packet)
	}
	return 1
}

// execFanout counts the messages a MsgExec dispatches, not counting the MsgExec
// itself. It does not recurse: the ante handler rejects a nested MsgExec, and
// counting runs before the ante handler, so recursing on attacker controlled
// nesting could exhaust the stack in PrepareProposal, which has no recover.
func execFanout(exec *authz.MsgExec) int {
	nested, err := exec.GetMessages()
	if err != nil {
		// The ante handler rejects a tx with undecodable inner messages, so
		// count the wrappers rather than treating them as free.
		return len(exec.Msgs)
	}
	count := 0
	for _, msg := range nested {
		count += leafWeight(msg)
	}
	return count
}

// icaExecuteTxPayload returns the CosmosTx payload of packet if the ICA host
// would execute it, or false if the packet is not bound for the host or does not
// carry an EXECUTE_TX. Such a packet dispatches no messages.
func icaExecuteTxPayload(packet channeltypes.Packet) ([]byte, bool) {
	if packet.DestinationPort != icatypes.HostPortID {
		return nil, false
	}
	var data icatypes.InterchainAccountPacketData
	if err := data.UnmarshalJSON(packet.Data); err != nil || data.Type != icatypes.EXECUTE_TX {
		return nil, false
	}
	return data.Data, true
}

// countICAPacketMsgs returns how many messages the ICA host could dispatch from
// packet: zero if the host does not execute it, or MaxSDKMessages if the
// payload decodes under neither encoding the host supports.
//
// The count does not read the channel's encoding, which can change before the
// packet executes. It is the larger of the proto3 and proto3json counts. Each
// counter accepts every payload the host's decoder for that encoding accepts,
// so a payload one counter rejects dispatches nothing under that encoding.
func countICAPacketMsgs(packet channeltypes.Packet) int {
	payload, ok := icaExecuteTxPayload(packet)
	if !ok {
		return 0
	}
	// Both counters return -1 when the payload does not decode.
	count := max(countProtoMsgs(payload), countJSONMsgs(payload))
	if count < 0 {
		return appconsts.MaxSDKMessages
	}
	return count
}

// countICAPacketZKProofMsgs returns how many ZK proof messages the ICA host
// could dispatch. Malformed host payloads count over the limit because ante
// validation occurs after proposal counting.
func countICAPacketZKProofMsgs(packet channeltypes.Packet) int {
	return countICAPacketWeighted(packet, zkProofTypeURLWeight, appconsts.MaxZKProofMessages+1)
}

// countICAPacketISMSignatures fails closed on a MsgProcessMessage in an ICA
// payload: the wire-level walk sees type URLs, not metadata lengths.
func countICAPacketISMSignatures(packet channeltypes.Packet) int {
	return countICAPacketWeighted(packet, ismTypeURLWeight, appconsts.MaxISMSignatures+1)
}

// countICAPacketWeighted sums weigh over the type URLs the ICA host could
// dispatch from packet under either encoding, like countICAPacketMsgs, or
// returns over if the payload decodes under neither.
func countICAPacketWeighted(packet channeltypes.Packet, weigh func(typeURL string) int, over int) int {
	payload, ok := icaExecuteTxPayload(packet)
	if !ok {
		return 0
	}
	count := max(countProtoWeighted(payload, weigh, over), countJSONWeighted(payload, weigh, over))
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

// countProtoWeighted sums weigh over a proto3 CosmosTx, or -1 if the host's
// decoder rejects the bytes.
func countProtoWeighted(bz []byte, weigh func(string) int, over int) int {
	count := 0
	fanOut := false
	ok := walkProtoMsg(bz, isCosmosTxMsgsField, func(_ int32, anyBz []byte) bool {
		typeURL, ok := protoAnyTypeURL(anyBz)
		if !ok {
			return false
		}
		if isFanOutTypeURL(typeURL) {
			fanOut = true
			return false
		}
		count += weigh(typeURL)
		return true
	})
	if fanOut {
		return over
	}
	if !ok {
		return -1
	}
	return count
}

// protoAnyTypeURL returns the type URL of a proto3 encoded Any, or false if the
// host's decoder rejects the bytes. Like the decoder, it keeps the last one.
func protoAnyTypeURL(bz []byte) (string, bool) {
	var typeURL string
	ok := walkProtoMsg(bz, isAnyField, func(num int32, value []byte) bool {
		if num == 1 {
			typeURL = string(value)
		}
		return true
	})
	return typeURL, ok
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
// host's decoder rejects the bytes. It walks the wire format instead of
// decoding, so the cost does not depend on how many entries there are.
func countProtoMsgs(bz []byte) int {
	count := 0
	ok := walkProtoMsg(bz, isCosmosTxMsgsField, func(int32, []byte) bool {
		count++
		return true
	})
	if !ok {
		return -1
	}
	return count
}

func isCosmosTxMsgsField(num int32) bool { return num == cosmosTxMsgsField }

func isAnyField(num int32) bool { return num == 1 || num == 2 }

// walkProtoMsg walks the top-level fields of a proto3 encoded message and calls
// visit with the value of each field that isField reports. It returns false if
// visit does, or if the gogoproto generated Unmarshal, which the ICA host uses,
// rejects the bytes. It mirrors that generated code: every isField field must
// be length delimited, and other fields, groups included, are skipped. Counting
// treats a payload this rejects as one the host dispatches nothing from, so it
// must accept everything the generated code accepts.
func walkProtoMsg(bz []byte, isField func(int32) bool, visit func(num int32, value []byte) bool) bool {
	l := len(bz)
	i := 0
	for i < l {
		start := i
		wire, next, ok := gogoVarint(bz, i)
		if !ok {
			return false
		}
		i = next
		// The generated code truncates the field number to int32.
		num := int32(wire >> 3) //nolint:gosec // mirrors the generated decoder
		wireType := wire & 0x7
		if wireType == 4 || num <= 0 {
			return false
		}
		if !isField(num) {
			n, ok := gogoSkip(bz[start:])
			if !ok || n < 0 || start+n < 0 || start+n > l {
				return false
			}
			i = start + n
			continue
		}
		if wireType != 2 {
			return false
		}
		length, next, ok := gogoVarint(bz, i)
		if !ok {
			return false
		}
		n := int(length) //nolint:gosec // mirrors the generated decoder
		if n < 0 || next+n < 0 || next+n > l {
			return false
		}
		if !visit(num, bz[next:next+n]) {
			return false
		}
		i = next + n
	}
	return true
}

// gogoVarint reads a varint at bz[i:] the way gogoproto generated code does:
// at most ten bytes, dropping bits past the 64th rather than rejecting them. It
// returns the value and the offset after it.
func gogoVarint(bz []byte, i int) (uint64, int, bool) {
	var v uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if i >= len(bz) {
			return 0, 0, false
		}
		b := bz[i]
		i++
		v |= uint64(b&0x7F) << shift
		if b < 0x80 {
			return v, i, true
		}
	}
	return 0, 0, false
}

// gogoSkip returns the length of the field at the start of bz the way the
// gogoproto generated skip functions do. A group is skipped by counting start
// and end tags, not by recursing, so its nesting depth costs no stack.
func gogoSkip(bz []byte) (int, bool) {
	l := len(bz)
	i := 0
	depth := 0
	for i < l {
		wire, next, ok := gogoVarint(bz, i)
		if !ok {
			return 0, false
		}
		i = next
		switch wire & 0x7 {
		case 0:
			if _, i, ok = gogoVarint(bz, i); !ok {
				return 0, false
			}
		case 1:
			i += 8
		case 2:
			length, next, ok := gogoVarint(bz, i)
			if !ok {
				return 0, false
			}
			n := int(length) //nolint:gosec // mirrors the generated decoder
			if n < 0 {
				return 0, false
			}
			i = next + n
		case 3:
			depth++
		case 4:
			if depth == 0 {
				return 0, false
			}
			depth--
		case 5:
			i += 4
		default:
			return 0, false
		}
		if i < 0 {
			return 0, false
		}
		if depth == 0 {
			return i, true
		}
	}
	return 0, false
}

// countJSONMsgs counts the entries of a proto3json encoded CosmosTx, or -1 if
// the host's decoder rejects the bytes. Entries decode into an empty struct, so
// only the list length is retained.
func countJSONMsgs(bz []byte) int {
	raw, ok := jsonCosmosTxMsgs(bz)
	if !ok {
		return -1
	}
	var msgs []struct{}
	if raw != nil && json.Unmarshal(raw, &msgs) != nil {
		return -1
	}
	return len(msgs)
}

// countJSONWeighted sums weigh over a proto3json CosmosTx, or -1 if the host's
// decoder rejects the bytes.
func countJSONWeighted(bz []byte, weigh func(string) int, over int) int {
	raw, ok := jsonCosmosTxMsgs(bz)
	if !ok {
		return -1
	}
	// The decoder reads "@type" from a map, so a later duplicate key wins even
	// over a value of the wrong type. A RawMessage keeps that order and does not
	// fail on the first value's type.
	var msgs []struct {
		TypeURL json.RawMessage `json:"@type"`
	}
	if raw != nil && json.Unmarshal(raw, &msgs) != nil {
		return -1
	}
	count := 0
	for _, msg := range msgs {
		var typeURL string
		if msg.TypeURL != nil && json.Unmarshal(msg.TypeURL, &typeURL) != nil {
			return -1
		}
		if isFanOutTypeURL(typeURL) {
			return over
		}
		count += weigh(typeURL)
	}
	return count
}

// jsonCosmosTxMsgs returns the raw "messages" value of a proto3json CosmosTx, or
// false if the host's decoder rejects the bytes. It reads the payload the way
// that decoder (gogoproto jsonpb) does: only the first JSON value, ignoring any
// bytes after it, into a map, so a later duplicate key wins.
func jsonCosmosTxMsgs(bz []byte) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(bz)).Decode(&fields); err != nil {
		return nil, false
	}
	return fields["messages"], true
}
