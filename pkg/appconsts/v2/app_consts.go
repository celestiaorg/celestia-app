package v2

const (
	Version              uint64 = 2
	SquareSizeUpperBound int    = 128
	SubtreeRootThreshold int    = 64

	// GlobalMinGasPrice is used in the processProposal to ensure
	// that all transactions have a gas price greater than or equal to this value.
	GlobalMinGasPrice = 0.002 // same as DefaultMinGasPrice
)
