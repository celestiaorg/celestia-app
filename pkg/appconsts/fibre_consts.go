//go:build !benchmarks

package appconsts

const (
	// MaxPayForFibreMessages is the maximum number of PayForFibre messages that a block can contain.
	MaxPayForFibreMessages = 200
	// MaxPayForFibreCandidates is the maximum number of PayForFibre transactions a proposer
	// considers for one block. It is above MaxPayForFibreMessages because candidates can fail
	// the ante handler, fail to settle, or not fit the square.
	MaxPayForFibreCandidates = 2 * MaxPayForFibreMessages
)
