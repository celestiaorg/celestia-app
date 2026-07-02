package appconsts

// These constants are in a separate file from fibre_consts.go because they are
// referenced by both benchmark and non-benchmark builds.
// fibre_consts.go is gated behind //go:build !benchmarks, so any constants
// there are unavailable in benchmark builds.
const (
	// PFBFibreGasFixedCost is the fixed gas cost per blob in a PayForFibre transaction.
	PFBFibreGasFixedCost uint64 = 650_000

	// PFBFibreGasPerChunk is the gas cost per 256 KiB chunk in a PayForFibre transaction.
	PFBFibreGasPerChunk uint64 = 45_000

	// PFBFibreChunkSize is the chunk size (256 KiB) used for gas calculation in PayForFibre.
	PFBFibreChunkSize uint32 = 262_144
)
