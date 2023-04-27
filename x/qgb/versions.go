package qgb

import (
	v1 "github.com/celestiaorg/celestia-app/x/qgb/v1"
	"github.com/celestiaorg/celestia-app/x/qgb/v1beta1"
)

func GetSignificantPowerDiffThreshold(appVersion uint64) float64 {
	switch appVersion {
	case 0:
		return v1beta1.SignificantPowerDifferenceThreshold
	case 1:
		return v1.SignificantPowerDifferenceThreshold
	default:
		return v1.SignificantPowerDifferenceThreshold
	}
}
