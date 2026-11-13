package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	zkismtypes "github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/gogoproto/proto"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// maxFuzzMsgs bounds the messages one fuzz script builds.
const maxFuzzMsgs = 64

var icaEncodings = []string{icatypes.EncodingProtobuf, icatypes.EncodingProto3JSON}

// msgRecord is the bytes one message takes from the script: a kind byte,
// whose high bit sends an ICA packet to another port, and a size byte.
const msgRecord = 2

// fuzzMsgBuilder turns a byte script into messages, one fixed-width record
// per message.
type fuzzMsgBuilder struct {
	t       *testing.T
	cdc     codec.Codec
	script  *fuzzutil.Consumer
	payload []byte
	built   int
}

// next returns the kind and size of the next message.
func (b *fuzzMsgBuilder) next() (kind, size byte, ok bool) {
	if b.script.Exhausted() || b.built >= maxFuzzMsgs {
		return 0, 0, false
	}
	r := b.script.Record(msgRecord)
	b.built++
	return r.Byte(), r.Byte(), true
}

func (b *fuzzMsgBuilder) zkMsg(op byte) sdk.Msg {
	switch op % 4 {
	case 0:
		return &zkismtypes.MsgCreateInterchainSecurityModule{}
	case 1:
		return &zkismtypes.MsgUpdateInterchainSecurityModule{}
	case 2:
		return &zkismtypes.MsgSubmitMessages{}
	default:
		return &zkismtypes.MsgUpdateVerifyingKeys{}
	}
}

func (b *fuzzMsgBuilder) icaPacket(kind byte, payload []byte) sdk.Msg {
	data := icatypes.InterchainAccountPacketData{Type: icatypes.EXECUTE_TX, Data: payload}
	port := icatypes.HostPortID
	if kind&0x80 != 0 {
		port = "transfer"
	}
	packet := channeltypes.NewPacket(data.GetBytes(), 1, "icacontroller-test", "channel-0", port, "channel-1", clienttypes.ZeroHeight(), 0)
	return channeltypes.NewMsgRecvPacket(packet, nil, clienttypes.ZeroHeight(), "signer")
}

// msg builds one message, recursing into MsgExec to the given depth.
func (b *fuzzMsgBuilder) msg(depth int) (sdk.Msg, bool) {
	kind, sizeByte, ok := b.next()
	if !ok {
		return nil, false
	}
	size := int(sizeByte)
	switch kind & 7 {
	case 0, 7:
		return &banktypes.MsgSend{}, true
	case 1:
		if depth >= 2 {
			return &banktypes.MsgSend{}, true
		}
		var nested []sdk.Msg
		for range size % 6 {
			m, ok := b.msg(depth + 1)
			if !ok {
				break
			}
			nested = append(nested, m)
		}
		exec := authz.NewMsgExec(sdk.AccAddress{}, nested)
		return &exec, true
	case 2:
		requests := make([]*icahosttypes.QueryRequest, size%8)
		for i := range requests {
			requests[i] = &icahosttypes.QueryRequest{Path: "/cosmos.bank.v1beta1.Query/Balance"}
		}
		return &icahosttypes.MsgModuleQuerySafe{Requests: requests}, true
	case 3:
		msgs := make([]proto.Message, size%5)
		for i := range msgs {
			if (size+i)%2 == 0 {
				msgs[i] = &banktypes.MsgSend{}
			} else {
				msgs[i] = b.zkMsg(byte(i))
			}
		}
		payload, err := icatypes.SerializeCosmosTx(b.cdc, msgs, icaEncodings[size%2])
		require.NoError(b.t, err)
		return b.icaPacket(kind, payload), true
	case 4:
		return b.zkMsg(sizeByte), true
	case 5:
		n := min(size*8, len(b.payload))
		payload := b.payload[:n]
		b.payload = b.payload[n:]
		return b.icaPacket(kind, payload), true
	default:
		return &icahosttypes.MsgModuleQuerySafe{}, true
	}
}

// dispatched returns how many messages execution would run for msg, and how
// many of them perform ZK proof work, under the channel encoding enc. ok is
// false when the ante handler rejects the tx before execution.
func dispatched(cdc codec.Codec, enc string, msg sdk.Msg, nested bool) (msgs, zk int, ok bool) {
	switch msg := msg.(type) {
	case *authz.MsgExec:
		if nested {
			return 0, 0, false
		}
		inner, err := msg.GetMessages()
		if err != nil {
			return 0, 0, false
		}
		msgs = 1
		for _, m := range inner {
			n, z, ok := dispatched(cdc, enc, m, true)
			if !ok {
				return 0, 0, false
			}
			msgs += n
			zk += z
		}
		return msgs, zk, true
	case *icahosttypes.MsgModuleQuerySafe:
		return 1 + len(msg.Requests), 0, true
	case *channeltypes.MsgRecvPacket:
		if msg.Packet.DestinationPort != icatypes.HostPortID {
			return 1, 0, true
		}
		var data icatypes.InterchainAccountPacketData
		if err := data.UnmarshalJSON(msg.Packet.Data); err != nil || data.Type != icatypes.EXECUTE_TX {
			return 1, 0, true
		}
		hosted, err := icatypes.DeserializeCosmosTx(cdc, data.Data, enc)
		if err != nil {
			return 1, 0, true
		}
		for _, m := range hosted {
			if isZKProofTypeURL(sdk.MsgTypeURL(m)) {
				zk++
			}
		}
		return 1 + len(hosted), zk, true
	}
	if isZKProofTypeURL(sdk.MsgTypeURL(msg)) {
		return 1, 1, true
	}
	return 1, 0, true
}

// FuzzCountTxMsgs builds txs that mix sends, MsgExec wrappers, ICA query and
// packet messages and ZK messages, round-trips them through the app's tx
// decoder and checks the proposal message counters against what execution
// would dispatch. Counting more is safe; counting fewer lets a block execute
// messages and proofs its per-block limits never accounted for.
func FuzzCountTxMsgs(f *testing.F) {
	f.Add(msgScript(0, 0, 1, 3, 0, 0, 4, 0, 2, 5), []byte{}, false)
	f.Add(msgScript(3, 4, 3, 7, 1, 2, 3, 3, 4, 0), []byte{}, true)
	f.Add(msgScript(5, 4, 1, 1, 5, 2), []byte(`{"messages":[{"@type":"/cosmos.bank.v1beta1.MsgSend"}]}`), true)
	f.Add(msgScript(1, 5, 1, 5, 4, 0, 4, 0, 4, 0, 4, 0, 4, 0), []byte{}, false)
	f.Add(msgScript(2, 31, 1, 4, 2, 31, 2, 31, 3, 31), []byte{}, false)
	f.Add(msgScript(3|0x80, 4, 5|0x80, 2), []byte(`{}`), false)

	config := encoding.MakeConfig(ModuleEncodingRegisters...)
	f.Fuzz(func(t *testing.T, script, payload []byte, jsonChannel bool) {
		fuzzCountTxMsgs(t, config, script, payload, jsonChannel)
	})
}

// fuzzCountTxMsgs reports how many messages execution would dispatch, or -1
// when the tx does not decode or the ante handler would reject it.
func fuzzCountTxMsgs(t *testing.T, config encoding.Config, script, payload []byte, jsonChannel bool) int {
	t.Helper()
	if len(payload) > 1<<14 {
		return -1
	}
	cdc := config.Codec
	enc := icaEncodings[0]
	if jsonChannel {
		enc = icaEncodings[1]
	}
	b := &fuzzMsgBuilder{t: t, cdc: cdc, script: fuzzutil.New(script), payload: payload}
	var anys []*codectypes.Any
	for {
		m, ok := b.msg(0)
		if !ok {
			break
		}
		a, err := codectypes.NewAnyWithValue(m)
		require.NoError(t, err)
		anys = append(anys, a)
	}
	body, err := (&txtypes.TxBody{Messages: anys}).Marshal()
	require.NoError(t, err)
	raw, err := (&txtypes.TxRaw{BodyBytes: body}).Marshal()
	require.NoError(t, err)
	tx, err := config.TxConfig.TxDecoder()(raw)
	if err != nil {
		return -1
	}
	msgs := tx.GetMsgs()

	wantMsgs, wantZK := 0, 0
	for _, m := range msgs {
		n, z, ok := dispatched(cdc, enc, m, false)
		if !ok {
			return -1
		}
		wantMsgs += n
		wantZK += z
	}

	ck := fakeChannelKeeper{encoding: enc}
	if got := countExecutableMsgs(sdk.Context{}, ck, msgs); got <= appconsts.MaxSDKMessages {
		require.GreaterOrEqual(t, got, wantMsgs, "message count undercounts execution")
	}
	if got := countZKProofMsgs(sdk.Context{}, ck, msgs); got <= appconsts.MaxZKProofMessages {
		require.GreaterOrEqual(t, got, wantZK, "ZK proof count undercounts execution")
	}
	return wantMsgs
}

// TestCountTxMsgsSeeds checks that structured scripts survive the decoder and
// dispatch more than their top-level messages, so the fuzz target compares
// wrapped fan-out rather than rejected txs.
func TestCountTxMsgsSeeds(t *testing.T) {
	config := encoding.MakeConfig(ModuleEncodingRegisters...)
	// A send, a MsgExec of a send and a ZK message, and a query with five requests.
	require.Equal(t, 1+3+6, fuzzCountTxMsgs(t, config, msgScript(0, 0, 1, 2, 0, 0, 4, 0, 2, 5), nil, false))
	// An ICA packet of four messages under each channel encoding.
	require.Equal(t, 5, fuzzCountTxMsgs(t, config, msgScript(3, 4), nil, false))
	require.Equal(t, 5, fuzzCountTxMsgs(t, config, msgScript(3, 9), nil, true))
}

// msgScript lays out one record per kind and size pair.
func msgScript(kindSize ...byte) []byte {
	records := make([][]byte, 0, len(kindSize)/2)
	for i := 0; i+1 < len(kindSize); i += 2 {
		records = append(records, kindSize[i:i+2])
	}
	return fuzzutil.Records(msgRecord, records...)
}
