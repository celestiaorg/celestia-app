package appconsts

import (
	"time"
)

func GetTimeoutCommit(_ uint64) time.Duration {
	return TimeoutCommit
}

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
