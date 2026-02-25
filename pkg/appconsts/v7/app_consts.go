package v7

import "time"

const (
	Version              uint64 = 7
	SquareSizeUpperBound int    = 512
	SubtreeRootThreshold int    = 64
	// TimeoutPropose is the duration a proposer has to propose a block.
	TimeoutPropose = time.Millisecond * 8500
	// TimeoutProposeDelta is the increase in timeout propose per round.
	TimeoutProposeDelta = time.Millisecond * 500
	// TimeoutPrevote is the duration a node waits for a prevote.
	TimeoutPrevote = time.Millisecond * 3000
	// TimeoutPrevoteDelta is the increase in timeout prevote per round.
	TimeoutPrevoteDelta = time.Millisecond * 500
	// TimeoutPrecommit is the duration a node waits for a precommit.
	TimeoutPrecommit = time.Millisecond * 3000
	// TimeoutPrecommitDelta is the increase in timeout precommit per round.
	TimeoutPrecommitDelta = time.Millisecond * 500
	// TimeoutCommit is the duration a node waits after committing a block.
	TimeoutCommit = time.Millisecond
	// DelayedPrecommitTimeout is the time after a delayed precommit before
	// the node proceeds.
	DelayedPrecommitTimeout = time.Millisecond * 5850
)
