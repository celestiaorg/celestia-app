//go:build race

package app

import "time"

// maxFilterCost bounds the filter time for one 8 MiB tx under the race detector.
const maxFilterCost = 16 * time.Second
