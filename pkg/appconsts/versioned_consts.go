package appconsts

import (
	"time"
)

var TalisSquareSizeUpperBound = 512

func GetTimeoutCommit(_ uint64) time.Duration {
	return TimeoutCommit
}

func GetSquareSizeUpperBound(chainID string) int {
	if strings.Contains(chainID, TalisChainID) {
		return TalisSquareSizeUpperBound
	}
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
