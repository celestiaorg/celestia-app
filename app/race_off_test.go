//go:build !race

package app

import "time"

// maxFilterCost is a backstop for the filter time for one 8 MiB tx. The
// worst case takes about 0.6 s on a developer machine, and up to 2.2 s in a
// single run on a loaded CI runner, which is about three times slower.
const maxFilterCost = 5 * time.Second

// costRuns is the most times TestValidateDeterministicMsgsCost times each
// case before it keeps the best time.
const costRuns = 5
