//go:build !benchmarks

package appconsts

// ProcessProposal enforces these caps, so changing one needs an app version.
// They keep block processing under ~0.25s, except MaxISMSignatures (gas-priced; see its comment).
// The benchmarks behind them are in `app/benchmarks`.
const (
	// MaxPFBMessages is the maximum number of PFB messages that a block can contain.
	MaxPFBMessages = 200

	// MaxSDKMessages is the maximum number of SDK messages, aside from PFBs, that a block can contain.
	MaxSDKMessages = 300

	// MaxZKProofMessages bounds messages that verify a ZK proof or parse a verifying key in a block.
	MaxZKProofMessages = 64

	// MaxISMSignatures bounds Hyperlane multisig ISM signatures in a block, about
	// 90 ms of secp256k1 recovery with cgo or 350 ms in pure Go.
	MaxISMSignatures = 2048

	// MaxSDKGas bounds the summed gas limits of the txs MaxSDKMessages counts,
	// so per-block execution is bounded by work as well as by message count.
	MaxSDKGas = 100_000_000
)
