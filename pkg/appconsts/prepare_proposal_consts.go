//go:build !benchmarks

package appconsts

// The following consts are not consensus breaking and will be applied straight
// after this binary is started.
// These numbers softly constrain the processing time of blocks to 0.25sec.
// The benchmarks used to find these limits can be found in `app/benchmarks`.
const (
	// MaxPFBMessages is the maximum number of PFB messages that a block can contain.
	MaxPFBMessages = 200

	// MaxSDKMessages is the maximum number of SDK messages, aside from PFBs, that a block can contain.
	MaxSDKMessages = 600

	// MaxZKProofMessages bounds messages that verify a ZK proof or parse a verifying key in a block.
	MaxZKProofMessages = 64

	// MaxISMSignatures bounds the signatures carried by Hyperlane MsgProcessMessage
	// metadata in a block. Each costs one secp256k1 recovery when the ISM verifies.
	MaxISMSignatures = 256
)
