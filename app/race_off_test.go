//go:build !race

package app

import "time"

// maxFilterCost bounds the filter time for one 8 MiB tx.
const maxFilterCost = 2 * time.Second
