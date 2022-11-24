package types

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
)

// MsgSharesUsed calculates the minimum number of shares a message will take up.
// It accounts for the necessary delimiter and potential padding.
func MsgSharesUsed(msgSize int) int {
	// add the delimiter to the message size
	msgSize = shares.DelimLen(uint64(msgSize)) + msgSize
	shareCount := msgSize / appconsts.SparseShareContentSize
	// increment the share count if the message overflows the last counted share
	if msgSize%appconsts.SparseShareContentSize != 0 {
		shareCount++
	}
	return shareCount
}
