package appconsts

import (
	"time"

	"github.com/celestiaorg/go-square/v2/share"
)

// The following defaults correspond to initial parameters of the network that can be changed, not via app versions
// but other means such as on-chain governance, or the nodes local config
const (
	// DefaultGovMaxSquareSize is the default value for the governance modifiable
	// max square size.
	DefaultGovMaxSquareSize = 128

	// DefaultMaxBytes is the default value for the governance modifiable
	// maximum number of bytes allowed in a valid block.
	DefaultMaxBytes = DefaultGovMaxSquareSize * DefaultGovMaxSquareSize * share.ContinuationSparseShareContentSize

	// DefaultMinGasPrice is the default min gas price that gets set in the app.toml file.
	// The min gas price acts as a filter. Transactions below that limit will not pass
	// a nodes `CheckTx` and thus not be proposed by that node.
	DefaultMinGasPrice = 0.002 // utia

	// DefaultUnbondingTime is the default time a validator must wait
	// to unbond in a proof of stake system. Any validator within this
	// time can be subject to slashing under conditions of misbehavior.
	DefaultUnbondingTime = 3 * 7 * 24 * time.Hour

	// DefaultNetworkMinGasPrice is used by x/minfee to prevent transactions from being
	// included in a block if they specify a gas price lower than this.
	// Only applies to app version >= 2
	DefaultNetworkMinGasPrice = 0.000001 // utia
)

var DefaultUpperBoundMaxBytes = DefaultSquareSizeUpperBound * DefaultSquareSizeUpperBound * share.ContinuationSparseShareContentSize
