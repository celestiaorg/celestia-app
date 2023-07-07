package appconsts

import (
	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
)

const (
	LatestVersion = v1.Version
)

// SubtreeRootThreshold works as a target upper bound for the number of subtree
// roots in the share commitment. If a blob contains more shares than this
// number, then the height of the subtree roots will increase by one so that the
// number of subtree roots in the share commitment decreases by a factor of two.
// This step is repeated until the number of subtree roots is less than the
// SubtreeRootThreshold.
//
// The rationale for this value is described in more detail in ADR-013.
func SubtreeRootThreshold(_ uint64) int {
	return v1.SubtreeRootThreshold
}

// SquareSizeUpperBound is the maximum original square width possible
// for a version of the state machine. The maximum is decided through
// governance. See `DefaultGovMaxSquareSize`.
func SquareSizeUpperBound(_ uint64) int {
	return v1.SquareSizeUpperBound
}

var (
	DefaultSubtreeRootThreshold = SubtreeRootThreshold(LatestVersion)
	DefaultSquareSizeUpperBound = SquareSizeUpperBound(LatestVersion)
)
