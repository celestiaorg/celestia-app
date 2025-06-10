package appconsts

import (
	"strings"
	"time"

	appv4 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v4" //nolint:review
)

const (
	LatestVersion = appv4.Version
)

var (
	SquareSizeUpperBound      = appv4.SquareSizeUpperBound
	TxSizeCostPerByte         = appv4.TxSizeCostPerByte
	GasPerBlobByte            = appv4.GasPerBlobByte
	Version                   = appv4.Version
	UpgradeHeightDelay        = appv4.UpgradeHeightDelay
	MaxTxSize                 = appv4.MaxTxSize
	SubtreeRootThreshold      = appv4.SubtreeRootThreshold
	TimeoutCommit             = appv4.TimeoutCommit
	TimeoutPropose            = appv4.TimeoutPropose
	TalisSquareSizeUpperBound = 512
)

func GetTimeoutCommit(_ uint64) time.Duration {
	return appv4.TimeoutCommit
}

func GetSquareSizeUpperBound(chainID string) int {
	if strings.Contains(chainID, TalisChainID) {
		return 512
	}
	return appv4.SquareSizeUpperBound
}

// GetUpgradeHeightDelay returns the delay in blocks after a quorum has been
// reached that the chain should upgrade to the new version.
func GetUpgradeHeightDelay(chainID string) int64 {
	if chainID == TestChainID {
		return 3
	}
	return appv4.UpgradeHeightDelay
}
