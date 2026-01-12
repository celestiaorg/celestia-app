package types

import (
	"github.com/cosmos/gogoproto/proto"
)

// EventTypeBurnName is the typed event name for EventBurn.
var EventTypeBurnName = proto.MessageName(&EventBurn{})

// NewBurnEvent returns a new EventBurn.
func NewBurnEvent(burner string, amount string) *EventBurn {
	return &EventBurn{
		Burner: burner,
		Amount: amount,
	}
}
