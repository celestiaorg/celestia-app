package appconsts

import (
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v4"
)

const (
	LatestVersion = v4.Version
)

var (
	SquareSizeUpperBound      = v4.SquareSizeUpperBound
	TalisSquareSizeUpperBound = 512
	TxSizeCostPerByte         = v4.TxSizeCostPerByte
	GasPerBlobByte            = v4.GasPerBlobByte
	Version                   = v4.Version
	MaxTxSize                 = v4.MaxTxSize
	SubtreeRootThreshold      = v4.SubtreeRootThreshold
	TimeoutCommit             = v4.TimeoutCommit
	TimeoutPropose            = v4.TimeoutPropose

	TestUpgradeHeightDelay    = v4.TestUpgradeHeightDelay
	ArabicaUpgradeHeightDelay = v4.ArabicaUpgradeHeightDelay
	MochaUpgradeHeightDelay   = v4.MochaUpgradeHeightDelay
	MainnetUpgradeHeightDelay = v4.MainnetUpgradeHeightDelay
	// Deprecated: Use MainnetUpgradeHeightDelay instead.
	UpgradeHeightDelay = v4.MainnetUpgradeHeightDelay
)

func GetTimeoutCommit(_ uint64) time.Duration {
	return v4.TimeoutCommit
}

func GetSquareSizeUpperBound(chainID string) int {
	if strings.Contains(chainID, TalisChainID) {
		return TalisSquareSizeUpperBound
	}
	return v4.SquareSizeUpperBound
}

// GetUpgradeHeightDelay returns the delay in blocks after a quorum has been
// reached that the chain should upgrade to the new version.
func GetUpgradeHeightDelay(chainID string) int64 {
	if chainID == TestChainID {
		return v4.TestUpgradeHeightDelay
	}
	if chainID == ArabicaChainID {
		return v4.ArabicaUpgradeHeightDelay
	}
	if chainID == MochaChainID {
		return v4.MochaUpgradeHeightDelay
	}
	return v4.MainnetUpgradeHeightDelay
}
