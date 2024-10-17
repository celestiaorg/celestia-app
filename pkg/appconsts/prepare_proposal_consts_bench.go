//go:build bench_prepare_proposal

package appconsts

// Note: these constants are set to these values only when running benchmarks.
// For the production values, check prepare_proposal_consts.go file.

// The following consts are not consensus breaking and will be applied straight after this binary is started.
const (
	// NonPFBTransactionCap arbitrary high numbers for running benchmarks.
	NonPFBTransactionCap = 9999999999

	// PFBTransactionCap arbitrary high numbers for running benchmarks.
	PFBTransactionCap = 9999999999
)
