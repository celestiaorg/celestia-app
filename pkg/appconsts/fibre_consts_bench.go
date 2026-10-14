//go:build benchmarks

package appconsts

const (
	// MaxPayForFibreMessages arbitrary high numbers for running benchmarks.
	MaxPayForFibreMessages = 999999999999
	// MaxPayForFibreCandidates arbitrary high numbers for running benchmarks.
	MaxPayForFibreCandidates = 2 * MaxPayForFibreMessages
)
