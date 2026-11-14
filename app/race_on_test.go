//go:build race

package app

import "time"

// maxFilterCost is a backstop for the filter time for one 8 MiB tx under the
// race detector. The worst case takes about 5 s on a developer machine, and
// a CI runner can be about three times slower.
const maxFilterCost = 40 * time.Second

// costRuns is the most times TestValidateDeterministicMsgsCost times each
// case before it keeps the best time. It is lower under the race detector,
// where each run is slow.
const costRuns = 3
