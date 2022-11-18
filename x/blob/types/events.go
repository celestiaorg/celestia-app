package types

import "github.com/cosmos/gogoproto/proto"

var EventTypePayForBlob = proto.MessageName(&EventPayForBlob{})

// NewPayForBlobEvent constructs a new pay for blob sdk.Event
func NewPayForBlobEvent(signer string, size uint64) *EventPayForBlob {
	return &EventPayForBlob{
		Signer:   signer,
		BlobSize: size,
	}
}
