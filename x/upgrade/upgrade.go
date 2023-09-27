package upgrade

type Schedule map[string]map[int64]uint64 // chain-id -> height -> app version

// ShouldUpgradeNextHeight returns true if the network of the given chainID should
// modify the app version in the following block. This can be used for both upgrading
// and downgrading. This relies on social consensus to work. At least 2/3+ of the
// validators must have the same app version and height in their schedule for the
// upgrade to happen successfully.
func (k Keeper) ShouldUpgradeNextHeight(chainID string, height int64) bool {
	if k.upgradeSchedule == nil {
		return false
	}
	if schedule, ok := k.upgradeSchedule[chainID]; ok {
		if _, ok := schedule[height+1]; ok {
			return true
		}
	}
	return false
}

// GetAppVersionForNextHeight returns the app version that the network of the given
// chainID should modify in the following block. This is 0, if none is provided.
func (k Keeper) GetAppVersionForNextHeight(chainID string, height int64) uint64 {
	if k.upgradeSchedule == nil {
		return 0
	}
	if schedule, ok := k.upgradeSchedule[chainID]; ok {
		if appVersion, ok := schedule[height+1]; ok {
			return appVersion
		}
	}
	return 0
}
