package appconsts

import "time"

const (
	TimeoutPropose = time.Second * 10
	TimeoutCommit  = time.Second * 11
	GoalBlockTime  = time.Second * 15
)
