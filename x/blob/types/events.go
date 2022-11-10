package types

import (
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	EventTypePayForData = "payfordata"

	AttributeKeySigner = "signer"
	AttributeKeySize   = "size"
)

// NewPayForDataEvent constructs a new payfordata sdk.Event
func NewPayForDataEvent(signer string, size uint64) sdk.Event {
	return sdk.NewEvent(
		EventTypePayForData,
		sdk.NewAttribute(AttributeKeySigner, signer),
		sdk.NewAttribute(AttributeKeySize, strconv.FormatUint(size, 10)),
	)
}
