package upgrade

import fmt "fmt"

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

func (s Schedule) ValidateBasic() error {
	lastHeight := 0
	lastVersion := uint64(0)
	for idx, plan := range s {
		if err := plan.ValidateBasic(); err != nil {
			return fmt.Errorf("plan %d: %w", idx, err)
		}
		if plan.Start <= int64(lastHeight) {
			return fmt.Errorf("plan %d: start height must be greater than %d, got %d", idx, lastHeight, plan.Start)
		}
		if plan.Version <= lastVersion {
			return fmt.Errorf("plan %d: version must be greater than %d, got %d", idx, lastVersion, plan.Version)
		}
	}
	return nil
}

func (s Schedule) ShouldProposeUpgrade(height int64) (uint64, bool) {
	for _, plan := range s {
		if height >= plan.Start-1 && height < plan.End {
			return plan.Version, true
		}
	}
	return 0, false
}

func (p Plan) ValidateBasic() error {
	if p.Start < 1 {
		return fmt.Errorf("plan start height cannot be negative or zero: %d", p.Start)
	}
	if p.End < 1 {
		return fmt.Errorf("plan end height cannot be negative or zero: %d", p.End)
	}
	if p.Start >= p.End {
		return fmt.Errorf("plan end height must be greater than start height: %d >= %d", p.Start, p.End)
	}
	if p.Version == 0 {
		return fmt.Errorf("plan version cannot be zero")
	}
	return nil
}

func NewSchedule(plans ...Plan) Schedule {
	return plans
}

func NewPlan(startHeight, endHeight int64, version uint64) Plan {
	return Plan{
		Start:   startHeight,
		End:     endHeight,
		Version: version,
	}
}
