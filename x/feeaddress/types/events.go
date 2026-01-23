package types

// NewProtocolFeePaidEvent returns a new EventProtocolFeePaid.
func NewProtocolFeePaidEvent(fromAddress string, amount string) *EventProtocolFeePaid {
	return &EventProtocolFeePaid{
		FromAddress: fromAddress,
		Amount:      amount,
	}
}
