package v1

import "time"

const (
	Version              uint64 = 1
	SquareSizeUpperBound int    = 128
	SubtreeRootThreshold int    = 64
	TimeoutPropose              = time.Millisecond * 10000
	TimeoutCommit               = time.Millisecond * 11000
)
