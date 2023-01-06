package types

import "github.com/cosmos/gogoproto/proto"

var EventTypePayForBlob = proto.MessageName(&EventPayForBlob{})

// NewPayForBlobEvent returns a new EventPayForBlob
func NewPayForBlobEvent(signer string, blobSize uint32) *EventPayForBlob {
	return &EventPayForBlob{
		Signer:   signer,
		BlobSize: blobSize,
	}
}
