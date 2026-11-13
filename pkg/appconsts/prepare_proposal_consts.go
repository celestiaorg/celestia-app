//go:build !benchmarks

package appconsts

// The following consts are not consensus breaking and will be applied straight
// after this binary is started.
// The message caps softly constrain the processing time of blocks to 0.25sec,
// except MaxISMSignatures, which relies on gas pricing instead; see its comment.
// The benchmarks used to find these limits can be found in `app/benchmarks`.
const (
	// MaxPFBMessages is the maximum number of PFB messages that a block can contain.
	MaxPFBMessages = 200

	// MaxSDKMessages is the maximum number of SDK messages, aside from PFBs, that a block can contain.
	MaxSDKMessages = 600

	// MaxZKProofMessages bounds messages that verify a ZK proof or parse a verifying key in a block.
	MaxZKProofMessages = 64

	// MaxISMSignatures bounds Hyperlane multisig ISM signatures in a block, about
	// 90 ms of secp256k1 recovery with cgo or 350 ms in pure Go.
	MaxISMSignatures = 2048

	// MaxSDKGas bounds the summed gas limits of the txs MaxSDKMessages counts,
	// so per-block execution is bounded by work as well as by message count.
	MaxSDKGas = 100_000_000
)
