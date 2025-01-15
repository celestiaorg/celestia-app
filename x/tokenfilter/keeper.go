package tokenfilter

import (
	porttypes "github.com/cosmos/ibc-go/v9/modules/core/05-port/types"
)

// Keeper is so far a noop as the tokenfilter doesn't have any need to
// act as middleware for outgoing messages (only inbound ones).
type Keeper struct {
	porttypes.ICS4Wrapper
}

// NewKeeper creates a new tokenfilter Keeper instance.
func NewKeeper(wrapper porttypes.ICS4Wrapper) Keeper {
	return Keeper{
		ICS4Wrapper: wrapper,
	}
}
