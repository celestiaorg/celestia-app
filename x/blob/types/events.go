package types

import "github.com/cosmos/gogoproto/proto"

var EventTypePayForBlob = proto.MessageName(&EventPayForBlobs{})

// NewPayForBlobEvent returns a new EventPayForBlob
func NewPayForBlobsEvent(signer string, blobSize uint32, namespaceIDs [][]byte) *EventPayForBlobs {
	return &EventPayForBlobs{
		Signer:       signer,
		BlobSize:     blobSize,
		NamespaceIds: namespaceIDs,
	}
}
