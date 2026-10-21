package appconsts

// GetUpgradeHeightDelay returns the delay in blocks after a quorum has been
// reached that the chain should upgrade to the new version.
func GetUpgradeHeightDelay(chainID string) int64 {
	if chainID == TestChainID {
		return TestUpgradeHeightDelay
	}
	if chainID == CortoChainID {
		return CortoUpgradeHeightDelay
	}
	if chainID == MochaChainID {
		return MochaUpgradeHeightDelay
	}
	return MainnetUpgradeHeightDelay
}
