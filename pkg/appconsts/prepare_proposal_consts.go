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

	// MaxPFBCandidates is the maximum number of blob transactions a proposer considers for one
	// block. It is above MaxPFBMessages because candidates can fail validation or not fit the square.
	MaxPFBCandidates = 2 * MaxPFBMessages

	// MaxSDKCandidates is the maximum number of other non-PayForFibre transactions a proposer
	// considers for one block. It is above MaxSDKMessages for the same reason.
	MaxSDKCandidates = 2 * MaxSDKMessages
)
