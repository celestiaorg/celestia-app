package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	EventTypePayForMessage = "payformessage"

	AttributeKeySpender = "spender"
	AttributeKeySize    = "size"
)

//NewPayForMessageEvent construt a new payformessge sdk.Event
func NewPayforMessageEvent(spender sdk.AccAddress, size uint64) sdk.Event {
	return sdk.NewEvent(
		EventTypePayForMessage,
		sdk.NewAttribute(AttributeKeySpender, spender.String()),
		sdk.NewAttribute(AttributeKeySize, string(size)),
	)
}
