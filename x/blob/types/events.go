package types

import "github.com/cosmos/gogoproto/proto"

var EventTypePayForBlob = proto.MessageName(&EventPayForBlobs{})

// NewPayForBlobsEvent returns a new EventPayForBlobs
func NewPayForBlobsEvent(signer string, blobSizes []uint32, namespaceIDs [][]byte) *EventPayForBlobs {
	return &EventPayForBlobs{
		Signer:       signer,
		BlobSizes:    blobSizes,
		NamespaceIds: namespaceIDs,
	}
}
