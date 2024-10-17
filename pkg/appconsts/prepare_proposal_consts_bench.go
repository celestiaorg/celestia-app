//go:build bench_prepare_proposal

package appconsts

// Note: these constants are set to these values only when running `bench_prepare_proposal` benchmarks.
// For the production values, check prepare_proposal_consts.go file.

const (
	// NonPFBTransactionCap arbitrary high numbers for running benchmarks.
	NonPFBTransactionCap = 999999999999

	// PFBTransactionCap arbitrary high numbers for running benchmarks.
	PFBTransactionCap = 999999999999
)
