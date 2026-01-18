package types

import (
	"github.com/cosmos/gogoproto/proto"
)

// EventTypeFeeForwardedName is the typed event name for EventFeeForwarded.
var EventTypeFeeForwardedName = proto.MessageName(&EventFeeForwarded{})

// NewFeeForwardedEvent returns a new EventFeeForwarded.
func NewFeeForwardedEvent(from string, amount string) *EventFeeForwarded {
	return &EventFeeForwarded{
		From:   from,
		Amount: amount,
	}
}
