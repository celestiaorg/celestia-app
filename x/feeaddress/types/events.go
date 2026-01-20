package types

import (
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// NewFeeForwardedEvent returns a new EventFeeForwarded.
// The 'to_module' field is always the fee collector module account.
func NewFeeForwardedEvent(from string, amount string) *EventFeeForwarded {
	return &EventFeeForwarded{
		From:     from,
		ToModule: authtypes.FeeCollectorName,
		Amount:   amount,
	}
}
