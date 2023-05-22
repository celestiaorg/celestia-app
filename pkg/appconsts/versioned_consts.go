package appconsts

import (
	"fmt"
	"runtime/debug"

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
func SubtreeRootThreshold(version uint64) int {
	switch version {
	case v1.Version:
		return v1.SubtreeRootThreshold
	default:
		panic(unsupportedVersion(version))
	}
}

// MaxSquareSize is the maximum original square width.
func MaxSquareSize(version uint64) int {
	switch version {
	case v1.Version:
		return v1.MaxSquareSize
	default:
		panic(unsupportedVersion(version))
	}
}

var (
	DefaultSubtreeRootThreshold = SubtreeRootThreshold(LatestVersion)
	DefaultMaxSquareSize        = MaxSquareSize(LatestVersion)
)

func unsupportedVersion(version uint64) string {
	return fmt.Sprintf("unsupported app version %d\n%s", version, string(debug.Stack()))
}
