package types

import "github.com/cosmos/gogoproto/proto"

var EventTypePayForBlob = proto.MessageName(&EventPayForBlob{})

// NewPayForBlobEvent returns a new EventPayForBlob
func NewPayForBlobEvent(signer string, size uint64) *EventPayForBlob {
	return &EventPayForBlob{
		Signer:   signer,
		BlobSize: size,
	}
}
