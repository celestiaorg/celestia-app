package v2

const (
	Version              uint64 = 2
	SquareSizeUpperBound int    = 512
	SubtreeRootThreshold int    = 64
	// NetworkMinGasPrice is used by x/minfee to prevent transactions from being
	// included in a block if they specify a gas price lower than this.
	NetworkMinGasPrice float64 = 0.000001 // utia
)
