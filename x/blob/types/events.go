package types

const (
	EventTypePayForBlob = "payforblob"

	AttributeKeySigner = "signer"
	AttributeKeySize   = "size"
)

// NewPayForBlobEvent constructs a new payforblob sdk.Event
func NewPayForBlobEvent(signer string, size uint64) *EventPayForBlob {
	return &EventPayForBlob{
		Signer:   signer,
		BlobSize: size,
	}
}
