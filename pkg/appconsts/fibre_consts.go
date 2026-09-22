//go:build !benchmarks

package appconsts

const (
	// MaxPayForFibreMessages is the maximum number of PayForFibre messages that a block can contain.
	// Enforced in PrepareProposal and ProcessProposal only, so it bounds forward
	// block validity and never changes the replay of an existing block. The
	// worst-case verification cost of a block is this times the 2/3 quorum
	// prefix of the validator set.
	MaxPayForFibreMessages = 2000
)
