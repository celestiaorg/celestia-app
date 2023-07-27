package types

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// BlobSizeUpperBound returns an upper bound for the blob size that can fit in
// a single data square. Since the returned value is an upper bound, it is
// possible that slightly smaller blob may not fit due to shares that aren't
// occupied by the blob (i.e. the PFB tx shares).
func BlobSizeUpperBound(appVersion uint64) int {
	// TODO: the actual max square size is app.GovSquareSizeUpperBound() but
	// that method isn't readily accessible outside celestia-app.
	squareSizeUpperBound := appconsts.SquareSizeUpperBound(appVersion)
	maxShares := squareSizeUpperBound * squareSizeUpperBound
	maxShareBytes := maxShares * appconsts.ContinuationSparseShareContentSize

	// TODO: MaxBytes is a governance modifiable consensus parameter so this
	// function should read the current value for the MaxBytes param instead of
	// using DefaultMaxBytes.
	maxBlockBytes := appconsts.DefaultMaxBytes

	return min(maxShareBytes, maxBlockBytes)
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
