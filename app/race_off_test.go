//go:build !race

package app

import "time"

// maxFilterCost is a backstop for the filter time for one 8 MiB tx. The worst case takes up to 2.2 s on loaded CI.
const maxFilterCost = 5 * time.Second

// costRuns is the most times TestValidateDeterministicMsgsCost times each case before it keeps the best time.
const costRuns = 5
