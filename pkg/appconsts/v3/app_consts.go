package v3

import "time"

const (
	Version              uint64 = 3
	SquareSizeUpperBound int    = 128
	SubtreeRootThreshold int    = 64
	TimeoutPropose              = time.Second * 11
	TimeoutCommit               = time.Second * 11
)
