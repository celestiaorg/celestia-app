package types

// NewFeeForwardedEvent returns a new EventFeeForwarded.
func NewFeeForwardedEvent(fromAddress string, amount string) *EventFeeForwarded {
	return &EventFeeForwarded{
		FromAddress: fromAddress,
		Amount:      amount,
	}
}
