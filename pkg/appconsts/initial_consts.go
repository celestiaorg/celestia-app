package appconsts

import (
	"time"
)

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
	DefaultMinGasPrice = 0.004 // utia

	// UnbondingTime is the time a validator must wait to unbond in a proof of
	// stake system. Any validator within this time can be subject to slashing
	// under conditions of misbehavior.
	//
	// Modified from 3 weeks to 14 days + 1 hour in CIP-037.
	UnbondingTime = 337 * time.Hour // (14 days + 1 hour)

	// DefaultNetworkMinGasPrice is used by x/minfee to prevent transactions from being
	// included in a block if they specify a gas price lower than this.
	// Only applies to app version >= 2
	DefaultNetworkMinGasPrice = 0.000001 // utia

	DefaultUpperBoundMaxBytes = 128 * mebibyte
)
