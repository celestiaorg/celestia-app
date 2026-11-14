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

// rejectPFMForwardMemo reports whether the wrapper must reject this packet
// before packet-forward-middleware runs. It decodes the packet the way
// packet-forward-middleware does: ICS-20 packet data, a memo with a
// non-null "forward" key, then the memo into a struct whose "forward" and
// "forward.timeout" fields match the same JSON keys. It rejects a forward
// memo when:
//   - the memo is outside the limits that pfmForwardMemoWithinLimits checks,
//   - the forward metadata decodes to nil, which packet-forward-middleware
//     does not handle, or
//   - the forward timeout is a JSON number outside the int64 range.
//
// A memo that packet-forward-middleware treats as a plain transfer, or
// fails to decode, is not rejected here.
func rejectPFMForwardMemo(packetData []byte) bool {
	var data transfertypes.FungibleTokenPacketData
	if err := transfertypes.ModuleCdc.UnmarshalJSON(packetData, &data); err != nil {
		return false
	}
	d := make(map[string]any)
	if err := json.Unmarshal([]byte(data.Memo), &d); err != nil || d["forward"] == nil {
		return false
	}
	// Check the limits before any other decoding: packet-forward-middleware
	// decodes forward.next even when a later field is invalid.
	if !pfmForwardMemoWithinLimits(data.Memo) {
		return true
	}
	var memo struct {
		Forward *struct {
			Timeout pfmTimeout `json:"timeout"`
		} `json:"forward"`
	}
	// Any error here also makes packet-forward-middleware fail to parse the
	// memo, so it returns an error acknowledgement anyway.
	if err := json.Unmarshal([]byte(data.Memo), &memo); err != nil {
		return false
	}
	if memo.Forward == nil {
		// A later key that matches "forward" without case, set to null,
		// leaves no forward metadata.
		return true
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
