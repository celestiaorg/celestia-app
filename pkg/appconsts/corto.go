package appconsts

import "time"

const (
	CortoUnbondingTime  = 10 * time.Minute
	CortoMaxAgeDuration = CortoUnbondingTime
	// Both evidence bounds must expire. This assumes average blocks <= 1.2s,
	// like the global evidence policy; prolonged stalls can extend the window.
	CortoMaxAgeNumBlocks int64 = 500
	// CortoV12UpgradeHeightDelay is approximately one hour at 800ms/block.
	CortoV12UpgradeHeightDelay int64 = 4_500
)

// CortoV12Active selects the Corto policy from the committed chain/version.
func CortoV12Active(chainID string, appVersion uint64) bool {
	return chainID == CortoChainID && appVersion >= 12
}

// GetUnbondingTime returns the unbonding time permitted by governance.
func GetUnbondingTime(chainID string, appVersion uint64) time.Duration {
	if CortoV12Active(chainID, appVersion) {
		return CortoUnbondingTime
	}
	return UnbondingTime
}

// GetEvidenceAgeLimits returns the chain/version's duration and block limits.
func GetEvidenceAgeLimits(chainID string, appVersion uint64) (time.Duration, int64) {
	if CortoV12Active(chainID, appVersion) {
		return CortoMaxAgeDuration, CortoMaxAgeNumBlocks
	}
	return MaxAgeDuration, MaxAgeNumBlocks
}
