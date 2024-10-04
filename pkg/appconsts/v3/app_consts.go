package v3

const (
	Version              uint64 = 3
	SquareSizeUpperBound int    = 128
	SubtreeRootThreshold int    = 64
	TxSizeCostPerByte    uint64 = 10
	GasPerBlobByte       uint32 = 8
	// MsgSendTransactionCap maximum number of msg send transactions that a block can contain
	MsgSendTransactionCap = 3200

	// PFBTransactionCap maximum number of PFB messages a block can contain
	PFBTransactionCap = 2700
)
