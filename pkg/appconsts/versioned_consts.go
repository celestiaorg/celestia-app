package appconsts

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts/testground"
	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
)

const (
	LatestVersion = v2.Version
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

// GlobalMinGasPrice is used in the processProposal to ensure
// that all transactions have a gas price greater than or equal to this value.
func GlobalMinGasPrice(version uint64) (float64, error) {
	switch version {
	case v2.Version:
		return v2.GlobalMinGasPrice, nil
	default:
		return 0, fmt.Errorf("global min gas price not defined for version %d", version)
	}
}

// SquareSizeUpperBound is the maximum original square width possible
// for a version of the state machine. The maximum is decided through
// governance. See `DefaultGovMaxSquareSize`.
func SquareSizeUpperBound(v uint64) int {
	switch v {
	case testground.Version:
		return testground.SquareSizeUpperBound
	// There is currently only a single square size upper bound.
	default:
		return v1.SquareSizeUpperBound
	}
}

var (
	DefaultSubtreeRootThreshold = SubtreeRootThreshold(LatestVersion)
	DefaultSquareSizeUpperBound = SquareSizeUpperBound(LatestVersion)
)
