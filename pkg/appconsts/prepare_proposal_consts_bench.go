//go:build bench_abci_methods

package appconsts

// Note: these constants are set to these values only when running `bench_abci_methods` benchmarks.
// For the production values, check prepare_proposal_consts.go file.

const (
	// MaxPFBMessages arbitrary high numbers for running benchmarks.
	MaxPFBMessages = 999999999999

	// MaxNonPFBMessages arbitrary high numbers for running benchmarks.
	MaxNonPFBMessages = 999999999999
)
