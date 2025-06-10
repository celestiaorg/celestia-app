package appconsts

import (
	"strings"
	"time"

	v4 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v4" //nolint:review
)

const (
	LatestVersion = v4.Version
)

var (
	SquareSizeUpperBound      = v4.SquareSizeUpperBound
	TxSizeCostPerByte         = v4.TxSizeCostPerByte
	GasPerBlobByte            = v4.GasPerBlobByte
	Version                   = v4.Version
	UpgradeHeightDelay        = v4.UpgradeHeightDelay
	MaxTxSize                 = v4.MaxTxSize
	SubtreeRootThreshold      = v4.SubtreeRootThreshold
	TimeoutCommit             = v4.TimeoutCommit
	TimeoutPropose            = v4.TimeoutPropose
	TalisSquareSizeUpperBound = 512
)

func GetTimeoutCommit(_ uint64) time.Duration {
	return v4.TimeoutCommit
}

func GetSquareSizeUpperBound(chainID string) int {
	if strings.Contains(chainID, TalisChainID) {
		return 512
	}
	return v4.SquareSizeUpperBound
}

// GetUpgradeHeightDelay returns the delay in blocks after a quorum has been
// reached that the chain should upgrade to the new version.
func GetUpgradeHeightDelay(chainID string) int64 {
	if chainID == TestChainID {
		return 3
	}
	return v4.UpgradeHeightDelay
}
