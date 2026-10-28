//go:build !benchmarks

package appconsts

const (
	// MaxPayForFibreMessages is the maximum number of PayForFibre messages that a block can contain.
	MaxPayForFibreMessages = 200
	// MaxPayForFibreCandidates is the number of PayForFibre transactions a proposer pre-verifies
	// for one block. It bounds that warm-up, not inclusion: the fill keeps going past it.
	MaxPayForFibreCandidates = 2 * MaxPayForFibreMessages
)
