package types

import (
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	EventTypePayForMessage = "payformessage"

	AttributeKeySigner = "signer"
	AttributeKeySize   = "size"
)

//NewPayForMessageEvent construt a new payformessge sdk.Event
func NewPayForMessageEvent(signer string, size uint64) sdk.Event {
	return sdk.NewEvent(
		EventTypePayForMessage,
		sdk.NewAttribute(AttributeKeySigner, signer),
		sdk.NewAttribute(AttributeKeySize, strconv.FormatUint(size, 10)),
	)
}
