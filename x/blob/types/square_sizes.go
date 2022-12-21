package types

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
)

// BlobSharesUsed calculates the minimum number of shares a blob will take up.
// It accounts for the necessary delimiter and potential padding.
func BlobSharesUsed(blobSize int) int {
	// add the delimiter to the blob size
	blobSize = shares.DelimLen(uint64(blobSize)) + blobSize
	shareCount := blobSize / appconsts.ContinuationSparseShareContentSize
	// increment the share count if the blob overflows the last counted share
	if blobSize%appconsts.ContinuationSparseShareContentSize != 0 {
		shareCount++
	}
	return shareCount
}
