package appconsts

import (
	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
)

const (
	LatestVersion = v1.Version
)

// SubtreeRootThreshold works as a target value for the number of subtree roots in the
// share commitment. If a blob contains more shares than this number, then the height
// of the subtree roots will gradually increase so that the amount remains within that limit.
// The rationale for this value is described in more detail in ADR013
// (./docs/architecture/adr-013).
// ADR013 https://github.com/celestiaorg/celestia-app/blob/e905143e8fe138ce6085ae9a5c1af950a2d87638/docs/architecture/adr-013-non-interactive-default-rules-for-zero-padding.md //nolint: lll
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
