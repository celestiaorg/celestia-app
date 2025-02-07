package appconsts

import (
	"strconv"
	"time"

	v1 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v2"
	v3 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v3"
	v4 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v4"
)

const (
	LatestVersion = v4.Version
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
	return v4.SubtreeRootThreshold
}

// SquareSizeUpperBound imposes an upper bound on the max effective square size.
func SquareSizeUpperBound(_ uint64) int {
	if OverrideSquareSizeUpperBoundStr != "" {
		parsedValue, err := strconv.Atoi(OverrideSquareSizeUpperBoundStr)
		if err != nil {
			panic("Invalid OverrideSquareSizeUpperBoundStr value")
		}
		return parsedValue
	}
	return v4.SquareSizeUpperBound
}

func TxSizeCostPerByte(_ uint64) uint64 {
	return v4.TxSizeCostPerByte
}

func GasPerBlobByte(_ uint64) uint32 {
	return v4.GasPerBlobByte
}

func MaxTxSize(_ uint64) int {
	return v4.MaxTxSize
}

var (
	DefaultSubtreeRootThreshold = SubtreeRootThreshold(LatestVersion)
	DefaultSquareSizeUpperBound = SquareSizeUpperBound(LatestVersion)
	DefaultTxSizeCostPerByte    = TxSizeCostPerByte(LatestVersion)
	DefaultGasPerBlobByte       = GasPerBlobByte(LatestVersion)
)

func GetTimeoutPropose(v uint64) time.Duration {
	switch v {
	case v1.Version:
		return v1.TimeoutPropose
	case v2.Version:
		return v2.TimeoutPropose
	case v3.Version:
		return v3.TimeoutPropose
	default:
		return v4.TimeoutPropose
	}
}

func GetTimeoutCommit(v uint64) time.Duration {
	switch v {
	case v1.Version:
		return v1.TimeoutCommit
	case v2.Version:
		return v2.TimeoutCommit
	case v3.Version:
		return v3.TimeoutCommit
	default:
		return v4.TimeoutCommit
	}
}

// UpgradeHeightDelay returns the delay in blocks after a quorum has been reached that the chain should upgrade to the new version.
func UpgradeHeightDelay(chainID string, v uint64) int64 {
	if chainID == TestChainID {
		return 3
	}
	switch v {
	case v1.Version:
		return v1.UpgradeHeightDelay
	case v2.Version:
		// ONLY ON ARABICA: don't return the v2 value even when the app version is
		// v2 on arabica. This is due to a bug that was shipped on arabica, where
		// the next version was used.
		if chainID == ArabicaChainID {
			return v4.UpgradeHeightDelay
		}
		return v2.UpgradeHeightDelay
	case v3.Version:
		return v3.UpgradeHeightDelay
	default:
		return v4.UpgradeHeightDelay
	}
}
