package v5

import "time"

const (
	Version uint64 = 5
	// SquareSizeUpperBound imposes an upper bound on the max effective square size.
	SquareSizeUpperBound int = 128
	// SubtreeRootThreshold works as a target upper bound for the number of subtree
	// roots in the share commitment. If a blob contains more shares than this
	// number, then the height of the subtree roots will increase by one so that the
	// number of subtree roots in the share commitment decreases by a factor of two.
	// This step is repeated until the number of subtree roots is less than the
	// SubtreeRootThreshold.
	//
	// The rationale for this value is described in more detail in ADR-013.
	SubtreeRootThreshold int = 64
	TimeoutPropose           = time.Millisecond * 3500
	TimeoutCommit            = time.Millisecond * 4200
)
