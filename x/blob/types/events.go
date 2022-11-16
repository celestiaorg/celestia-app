package types

import (
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	EventTypePayForBlob = "payforblob"

	AttributeKeySigner = "signer"
	AttributeKeySize   = "size"
)

// NewPayForBlobEvent constructs a new payforblob sdk.Event
func NewPayForBlobEvent(signer string, size uint64) sdk.Event {
	return sdk.NewEvent(
		EventTypePayForBlob,
		sdk.NewAttribute(AttributeKeySigner, signer),
		sdk.NewAttribute(AttributeKeySize, strconv.FormatUint(size, 10)),
	)
}
