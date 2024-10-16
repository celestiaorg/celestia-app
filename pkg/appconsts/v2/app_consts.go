package v2

const (
	Version              uint64 = 2
	SquareSizeUpperBound int    = 128
	SubtreeRootThreshold int    = 64
	// NonPFBTransactionCap is the maximum number of SDK messages, aside from
	// PFBs, that a block can contain.
	NonPFBTransactionCap = 200
	// PFBTransactionCap is the maximum number of PFB messages a block can
	// contain.
	PFBTransactionCap = 600
)
