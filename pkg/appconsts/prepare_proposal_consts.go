//go:build !bench_prepare_proposal

package appconsts

// The following consts are not consensus breaking and will be applied straight after this binary is started.
const (
	// NonPFBTransactionCap is the maximum number of SDK messages, aside from PFBs, that a block can contain.
	NonPFBTransactionCap = 200

	// PFBTransactionCap is the maximum number of PFB messages a block can contain.
	PFBTransactionCap = 600
)
