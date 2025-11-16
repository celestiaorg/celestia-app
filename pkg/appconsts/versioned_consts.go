package appconsts

import (
	"time"

	v5appconsts "github.com/celestiaorg/celestia-app/v6/pkg/appconsts/v5"
)

// GetSquareSizeUpperBound return the upper bound (consensus critical) square
// size given the chain-id. As of app version 5, all networks including
// testnetworks will have the same size, however in the past (and presumably
// in the future) this has not been the case hence this function existing.
func GetSquareSizeUpperBound(_ string) int {
	return SquareSizeUpperBound
}

// GetUpgradeHeightDelay returns the delay in blocks after a quorum has been
// reached that the chain should upgrade to the new version.
func GetUpgradeHeightDelay(chainID string) int64 {
	if chainID == TestChainID {
		return TestUpgradeHeightDelay
	}
	if chainID == ArabicaChainID {
		return ArabicaUpgradeHeightDelay
	}
	if chainID == MochaChainID {
		return MochaUpgradeHeightDelay
	}
	return MainnetUpgradeHeightDelay
}

func GetTimeoutCommit(appVersion uint64) time.Duration {
	switch appVersion {
	case 1:
		return 0 // v1 did not have timeout commit hard-coded
	case 2:
		return 0 // v2 did not have timeout commit hard-coded
	case 3:
		return v5appconsts.TimeoutCommit
	case 4:
		return v5appconsts.TimeoutCommit
	case 5:
		return v5appconsts.TimeoutCommit
	case 6:
		return TimeoutCommit
	default:
		return TimeoutCommit
	}
}

func GetTimeoutPropose(appVersion uint64) time.Duration {
	switch appVersion {
	case 1:
		return 0 // v1 did not have timeout propose hard-coded
	case 2:
		return 0 // v2 did not have timeout propose hard-coded
	case 3:
		return v5appconsts.TimeoutPropose
	case 4:
		return v5appconsts.TimeoutPropose
	case 5:
		return v5appconsts.TimeoutPropose
	case 6:
		return TimeoutPropose
	default:
		return TimeoutPropose
	}
}
