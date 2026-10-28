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

	// MaxISMSignatures bounds the signatures carried by Hyperlane MsgProcessMessage
	// metadata in a block, each one secp256k1 recovery when a multisig ISM verifies.
	// GasPerISMSignature prices each signature; this budget only caps the total
	// and is set high enough that MaxSDKMessages binds first for honest traffic.
	MaxISMSignatures = 8192
)
