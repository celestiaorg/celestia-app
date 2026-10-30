package appconsts

import "time"

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

// ConsensusTimeouts are the consensus timeouts the app returns to CometBFT.
type ConsensusTimeouts struct {
	TimeoutPropose          time.Duration
	TimeoutProposeDelta     time.Duration
	TimeoutPrevote          time.Duration
	TimeoutPrevoteDelta     time.Duration
	TimeoutPrecommit        time.Duration
	TimeoutPrecommitDelta   time.Duration
	TimeoutCommit           time.Duration
	DelayedPrecommitTimeout time.Duration
	DynamicTimeoutCommit    time.Duration
}

// BlockInterval is the expected block interval: the delayed precommit plus
// the sleep after commit, or the dynamic timeout commit floor if larger.
func (t ConsensusTimeouts) BlockInterval() time.Duration {
	return max(t.DelayedPrecommitTimeout+t.TimeoutCommit, t.DynamicTimeoutCommit)
}

// v10ConsensusTimeouts give a ~2.6s block: DelayedPrecommitTimeout + TimeoutCommit.
var v10ConsensusTimeouts = ConsensusTimeouts{
	TimeoutPropose:          3000 * time.Millisecond,
	TimeoutProposeDelta:     500 * time.Millisecond,
	TimeoutPrevote:          2000 * time.Millisecond,
	TimeoutPrevoteDelta:     500 * time.Millisecond,
	TimeoutPrecommit:        3000 * time.Millisecond,
	TimeoutPrecommitDelta:   500 * time.Millisecond,
	TimeoutCommit:           500 * time.Millisecond,
	DelayedPrecommitTimeout: 2100 * time.Millisecond,
}

// v11ConsensusTimeouts pace blocks with the 800ms dynamic timeout commit only.
// TimeoutCommit is 1ns because celestia-core reads 0 as "use the config value".
var v11ConsensusTimeouts = ConsensusTimeouts{
	TimeoutPropose:          1500 * time.Millisecond,
	TimeoutProposeDelta:     500 * time.Millisecond,
	TimeoutPrevote:          1000 * time.Millisecond,
	TimeoutPrevoteDelta:     500 * time.Millisecond,
	TimeoutPrecommit:        1500 * time.Millisecond,
	TimeoutPrecommitDelta:   500 * time.Millisecond,
	TimeoutCommit:           time.Nanosecond,
	DelayedPrecommitTimeout: 0,
	DynamicTimeoutCommit:    800 * time.Millisecond,
}

// GetConsensusTimeouts returns the consensus timeouts for appVersion.
func GetConsensusTimeouts(appVersion uint64) ConsensusTimeouts {
	if appVersion <= 10 {
		return v10ConsensusTimeouts
	}
	return v11ConsensusTimeouts
}
