package appconsts

// The following defaults correspond to initial parameters of the network that can be changed, not via app versions
// but other means such as on-chain governance, or the node's local config
const (
	mebibyte = 1_048_576 // bytes
	// DefaultGovMaxSquareSize is the default value for the governance modifiable
	// max square size.
	DefaultGovMaxSquareSize = 256

	// DefaultMaxBytes is the default value for the governance modifiable
	// maximum number of bytes allowed in a valid block.
	DefaultMaxBytes = 32 * mebibyte

	// DefaultMinGasPrice is the default min gas price that gets set in the app.toml file.
	// The min gas price acts as a filter. Transactions below that limit will not pass
	// a node's `CheckTx` and thus not be proposed by that node.
	DefaultMinGasPrice       = 0.004 // utia
	LegacyDefaultMinGasPrice = 0.002 // utia

	// DefaultNetworkMinGasPrice is used by x/minfee to prevent transactions from being
	// included in a block if they specify a gas price lower than this.
	// Only applies to app version >= 2
	DefaultNetworkMinGasPrice = 0.000001 // utia

	DefaultUpperBoundMaxBytes = 128 * mebibyte
)
