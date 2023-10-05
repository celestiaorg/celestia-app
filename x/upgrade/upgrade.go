package upgrade

type Schedule []Plan

type Plan struct {
	Start   int64
	End     int64
	Version uint64
}

// ShouldUpgradeNextHeight returns true if the network of the given chainID should
// modify the app version in the following block. This can be used for both upgrading
// and downgrading. This relies on social consensus to work. At least 2/3+ of the
// validators must have the same app version and height in their schedule for the
// upgrade to happen successfully.
func (k Keeper) ShouldProposeUpgrade(chainID string, height int64) (uint64, bool) {
	if k.upgradeSchedule == nil {
		return 0, false
	}
	if schedule, ok := k.upgradeSchedule[chainID]; ok {
		return schedule.ShouldProposeUpgrade(height)
	}
	return 0, false
}

func (k *Keeper) PrepareUpgradeAtEndBlock(version uint64) {
	k.pendingAppVersion = version
}

func (k *Keeper) ShouldUpgrade() bool {
	return k.pendingAppVersion != 0
}

func (k Keeper) GetNextAppVersion() uint64 {
	return k.pendingAppVersion
}

func (k *Keeper) MarkUpgradeComplete() {
	k.pendingAppVersion = 0
}
