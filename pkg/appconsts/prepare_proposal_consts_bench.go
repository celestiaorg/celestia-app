//go:build bench_abci_methods

package appconsts

// Note: these constants are set to these values only when running `bench_abci_methods` benchmarks.
// For the production values, check prepare_proposal_consts.go file.

const (
	// NonPFBTransactionCap arbitrary high numbers for running benchmarks.
	NonPFBTransactionCap = 999999999999

	// PFBTransactionCap arbitrary high numbers for running benchmarks.
	PFBTransactionCap = 999999999999
)
