package appconsts

import (
	"strconv"

	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
)

const (
	LatestVersion = v3.Version
)

// SubtreeRootThreshold works as a target upper bound for the number of subtree
// roots in the share commitment. If a blob contains more shares than this
// number, then the height of the subtree roots will increase by one so that the
// number of subtree roots in the share commitment decreases by a factor of two.
// This step is repeated until the number of subtree roots is less than the
// SubtreeRootThreshold.
//
// The rationale for this value is described in more detail in ADR-013.
func SubtreeRootThreshold(_ uint64) int {
	return v3.SubtreeRootThreshold
}

// SquareSizeUpperBound imposes an upper bound on the max effective square size.
func SquareSizeUpperBound(_ uint64) int {
	if OverrideSquareSizeUpperBoundStr != "" {
		parsedValue, err := strconv.Atoi(OverrideSquareSizeUpperBoundStr)
		if err != nil {
			panic("Invalid OverrideSquareSizeUpperBoundStr value")
		}
		return parsedValue
	}
	return v3.SquareSizeUpperBound
}

func TxSizeCostPerByte(_ uint64) uint64 {
	return v3.TxSizeCostPerByte
}

func GasPerBlobByte(_ uint64) uint32 {
	return v3.GasPerBlobByte
}

func MaxTxBytes(_ uint64) int {
	return v3.MaxTxBytes
}

var (
	DefaultSubtreeRootThreshold = SubtreeRootThreshold(LatestVersion)
	DefaultSquareSizeUpperBound = SquareSizeUpperBound(LatestVersion)
	DefaultTxSizeCostPerByte    = TxSizeCostPerByte(LatestVersion)
	DefaultGasPerBlobByte       = GasPerBlobByte(LatestVersion)
)
