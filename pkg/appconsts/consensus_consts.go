package appconsts

import "time"

const (
	// GoalBlockTime is the target time interval between blocks. Since the block
	// interval isn't enforced at consensus, the real block interval isn't
	// guaranteed to exactly match GoalBlockTime. GoalBlockTime is currently targeted
	// through static timeouts (i.e. TimeoutPropose, TimeoutCommit).
	GoalBlockTime = time.Second * 15
)
