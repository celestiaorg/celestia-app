package app

import (
	"encoding/json"
	"testing"

	"cosmossdk.io/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

func transferPacketData(t *testing.T, memo string) []byte {
	t.Helper()
	data, err := json.Marshal(ibctransfertypes.FungibleTokenPacketData{
		Denom: "uatom", Amount: "1", Sender: "remote", Receiver: "pfm", Memo: memo,
	})
	require.NoError(t, err)
	return data
}

// TestRejectPFMForwardMemo checks which forward memos the wrapper rejects
// before packet-forward-middleware runs: a numeric timeout outside the int64
// range, and a memo that has a "forward" key but decodes to no forward
// metadata.
func TestRejectPFMForwardMemo(t *testing.T) {
	testCases := []struct {
		name string
		memo string
		want bool
	}{
		{"no memo", ``, false},
		{"memo without forward", `{"wasm":{"timeout":1e19}}`, false},
		{"invalid memo json", `{"forward":{"timeout":1e19}`, false},
		{"forward null", `{"forward":null}`, false},
		{"forward not an object", `{"forward":5}`, false},
		{"no timeout", `{"forward":{"receiver":"r"}}`, false},
		{"string timeout", `{"forward":{"timeout":"10m"}}`, false},
		{"invalid string timeout", `{"forward":{"timeout":"1e19"}}`, false},
		{"bool timeout", `{"forward":{"timeout":true}}`, false},
		{"null timeout", `{"forward":{"timeout":null}}`, false},
		{"zero", `{"forward":{"timeout":0}}`, false},
		{"negative zero", `{"forward":{"timeout":-0}}`, false},
		{"fraction", `{"forward":{"timeout":1.5}}`, false},
		{"negative in range", `{"forward":{"timeout":-5}}`, false},
		{"min int64", `{"forward":{"timeout":-9223372036854775808}}`, false},
		{"largest float64 below 2^63", `{"forward":{"timeout":9223372036854774784}}`, false},
		{"max int64 literal rounds to 2^63", `{"forward":{"timeout":9223372036854775807}}`, true},
		{"2^63", `{"forward":{"timeout":9223372036854775808}}`, true},
		{"9.3e18", `{"forward":{"timeout":9.3e18}}`, true},
		{"1e19", `{"forward":{"timeout":1e19}}`, true},
		{"-1e19", `{"forward":{"timeout":-1e19}}`, true},
		{"next float64 below -2^63", `{"forward":{"timeout":-9223372036854777856}}`, true},
		{"does not fit in float64", `{"forward":{"timeout":1e400}}`, false},
		{"field name case", `{"forward":{"TimeOut":1e19}}`, true},
		{"forward key case", `{"forward":{},"Forward":{"timeout":1e19}}`, true},
		{"duplicate timeout, last in range", `{"forward":{"timeout":1e19,"timeout":5}}`, false},
		{"duplicate timeout, last out of range", `{"forward":{"timeout":5,"timeout":1e19}}`, true},
		{"timeout in next only", `{"forward":{"next":{"forward":{"timeout":1e19}}}}`, false},
		{"timeout in next string only", `{"forward":{"next":"{\"forward\":{\"timeout\":1e19}}"}}`, false},
		{"upper case exponent", `{"forward":{"timeout":1E19}}`, true},
		{"positive exponent sign", `{"forward":{"timeout":1e+19}}`, true},
		{"2^63 as integer digits", `{"forward":{"timeout":10000000000000000000}}`, true},
		{"1e308", `{"forward":{"timeout":1e308}}`, true},
		{"negative zero fraction", `{"forward":{"timeout":-0.0}}`, false},
		{"escaped field name", `{"forward":{"timeout":1e19}}`, true},
		{"array timeout", `{"forward":{"timeout":[1e19]}}`, false},
		{"object timeout", `{"forward":{"timeout":{"v":1e19}}}`, false},
		{"stringified forward", `{"forward":"{\"timeout\":1e19}"}`, false},
		{"duplicate forward merges", `{"forward":{"timeout":1e19},"forward":{"receiver":"r"}}`, true},
		{"duplicate forward, last timeout in range", `{"forward":{"timeout":1e19},"forward":{"timeout":5}}`, false},
		{"upper case forward first, lower case second", `{"Forward":{"timeout":1e19},"forward":{"receiver":"r"}}`, true},
		{"forward reset by null", `{"forward":{"timeout":1e19},"Forward":null,"forward":{"receiver":"r"}}`, false},
		{"lower case forward key null", `{"Forward":{"timeout":1e19},"forward":null}`, false},
		{"duplicate timeout, last does not fit in float64", `{"forward":{"timeout":5,"timeout":1e400}}`, false},
		{"duplicate timeout, last null", `{"forward":{"timeout":1e19,"timeout":null}}`, false},
		{"duplicate timeout, last string", `{"forward":{"timeout":1e19,"timeout":"10m"}}`, false},
		{"out of range with another invalid field", `{"forward":{"timeout":1e19,"retries":300}}`, true},
		// A memo with a non-null "forward" key is a forward memo. If the
		// forward metadata decodes to nil, the memo is rejected.
		{"Forward null after forward", `{"forward":{"receiver":"r"},"Forward":null}`, true},
		{"FORWARD null after forward", `{"forward":{"receiver":"r"},"FORWARD":null}`, true},
		{"escaped Forward null after forward", `{"forward":{"receiver":"r"},"Forw\u0061rd":null}`, true},
		{"Forward null after empty forward", `{"forward":{},"Forward":null}`, true},
		{"Forward null before forward", `{"Forward":null,"forward":{"receiver":"r"}}`, false},
		{"forward object then forward null", `{"forward":{"receiver":"r"},"forward":null}`, false},
		{"escaped forward null after forward", `{"forward":{"receiver":"r"},"forw\u0061rd":null}`, false},
		{"Forward object then forward null", `{"Forward":{"receiver":"r"},"forward":null}`, false},
		{"Forward null then invalid Forward", `{"forward":{"receiver":"r"},"Forward":null,"Forward":5}`, false},
		{"Forward null then Forward object", `{"forward":{"receiver":"r"},"Forward":null,"Forward":{"receiver":"r"}}`, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, rejectPFMForwardMemo(transferPacketData(t, tc.memo)))
		})
	}

	t.Run("not transfer packet data", func(t *testing.T) {
		for _, data := range [][]byte{nil, {}, []byte("not json"), []byte(`{"forward":{"timeout":1e19}}`), {0xff, 0x00}} {
			require.False(t, rejectPFMForwardMemo(data))
		}
	})
}

// TestPFMIBCMiddlewareRejectsForwardMemo checks that the wrapper answers
// before packet-forward-middleware runs. The wrapped middleware is a zero
// value, so calling it would panic.
func TestPFMIBCMiddlewareRejectsForwardMemo(t *testing.T) {
	for _, memo := range []string{
		`{"forward":{"receiver":"r","port":"transfer","channel":"channel-0","timeout":1e19}}`,
		`{"forward":{"receiver":"r","port":"transfer","channel":"channel-0"},"Forward":null}`,
	} {
		ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())
		packet := channeltypes.Packet{Data: transferPacketData(t, memo)}
		ack := pfmIBCMiddleware{}.OnRecvPacket(ctx, packet, nil)
		require.False(t, ack.Success())
		require.Equal(t, `{"error":"ABCI code: 11151: error handling packet: see events for details"}`, string(ack.Acknowledgement()))
		require.Empty(t, ctx.EventManager().Events())
	}
}
