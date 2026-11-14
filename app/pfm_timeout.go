package app

import (
	"encoding/json"

	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

// pfmTimeoutLimit is 2^63. A float64 timeout v converts to time.Duration
// (int64) with a result that the Go specification defines only when
// -pfmTimeoutLimit <= v < pfmTimeoutLimit. Outside that range the result
// depends on the CPU architecture.
const pfmTimeoutLimit = float64(1 << 63)

// hasOutOfRangePFMTimeout reports whether packet-forward-middleware would
// read the forward timeout of this packet from a JSON number outside the
// int64 range. It decodes the packet the way packet-forward-middleware does:
// ICS-20 packet data, a memo with a "forward" key, then the memo into a
// struct whose "forward.timeout" field matches the same JSON keys.
func hasOutOfRangePFMTimeout(packetData []byte) bool {
	var data transfertypes.FungibleTokenPacketData
	if err := transfertypes.ModuleCdc.UnmarshalJSON(packetData, &data); err != nil {
		return false
	}
	d := make(map[string]any)
	if err := json.Unmarshal([]byte(data.Memo), &d); err != nil || d["forward"] == nil {
		return false
	}
	var memo struct {
		Forward *struct {
			Timeout pfmTimeout `json:"timeout"`
		} `json:"forward"`
	}
	// Any error here also makes packet-forward-middleware fail to parse the
	// memo, so it returns an error acknowledgement anyway.
	if err := json.Unmarshal([]byte(data.Memo), &memo); err != nil || memo.Forward == nil {
		return false
	}
	return memo.Forward.Timeout.outOfRange
}

// pfmTimeout records whether a JSON timeout value is a number outside the
// int64 range. Like packet-forward-middleware's Duration, the last value of
// a repeated key wins.
type pfmTimeout struct {
	outOfRange bool
}

// UnmarshalJSON implements json.Unmarshaler. It never returns an error: a
// value that packet-forward-middleware cannot parse makes it return an error
// acknowledgement on its own.
func (t *pfmTimeout) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.outOfRange = false
		return nil
	}
	f, ok := v.(float64)
	t.outOfRange = ok && (f < -pfmTimeoutLimit || f >= pfmTimeoutLimit)
	return nil
}
