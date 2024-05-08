package v2

const (
	Version              uint64 = 2
	SquareSizeUpperBound int    = 128
	SubtreeRootThreshold int    = 64
	// GlobalMinGasPrice is used by x/minfee to prevent transactions from being
	// included in a block if they specify a gas price lower than this
	GlobalMinGasPrice float64 = 0.000001 // utia
)
