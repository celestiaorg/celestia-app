package types

import (
	"github.com/cosmos/gogoproto/proto"
)

var EventTypePayForBlob = proto.MessageName(&EventPayForBlobs{})

// NewPayForBlobsEvent returns a new EventPayForBlobs
func NewPayForBlobsEvent(signer string, blobSizes []uint32, namespaces [][]byte) *EventPayForBlobs {
	return &EventPayForBlobs{
		Signer:     signer,
		BlobSizes:  blobSizes,
		Namespaces: namespaces,
	}
}
