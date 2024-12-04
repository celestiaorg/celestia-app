//go:build !bench_abci_methods

package appconsts

// The following consts are not consensus breaking and will be applied straight
// after this binary is started.
// These numbers softly constrain the processing time of blocks to 0.25sec.
// The benchmarks used to find these limits can be found in `app/benchmarks`.
const (
	// MaxPFBMessages is the maximum number of PFB messages a block can contain.
	MaxPFBMessages = 200

	// MaxNonPFBMessages is the maximum number of SDK messages, aside from PFBs, that a block can contain.
	MaxNonPFBMessages = 600
)
