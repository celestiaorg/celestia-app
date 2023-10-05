package upgrade_test

import (
	fmt "fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/x/upgrade"
	"github.com/stretchr/testify/require"
)

func TestScheduleValidity(t *testing.T) {
	testCases := []struct {
		schedule upgrade.Schedule
		valid    bool
	}{
		// can be empty
		{upgrade.Schedule{}, true},
		// plan can not start at height 0
		{upgrade.Schedule{upgrade.Plan{Start: 0, End: 2, Version: 1}}, false},
		// end height can not be 0
		{upgrade.Schedule{upgrade.Plan{Version: 1}}, false},
		{upgrade.Schedule{upgrade.Plan{Start: 1, End: 2, Version: 1}}, true},
		// version can't be 0
		{upgrade.Schedule{upgrade.Plan{Start: 1, End: 2, Version: 0}}, false},
		// start and end height can be the same
		{upgrade.Schedule{upgrade.Plan{Start: 2, End: 2, Version: 1}}, true},
		// end height must be greater than start height
		{upgrade.Schedule{upgrade.Plan{Start: 2, End: 1, Version: 1}}, false},
		// plans can not overlap
		{upgrade.Schedule{upgrade.Plan{Start: 1, End: 2, Version: 1}, upgrade.Plan{Start: 2, End: 3, Version: 2}}, false},
		// plans must be in order. They can skip versions
		{upgrade.Schedule{upgrade.Plan{Start: 1, End: 2, Version: 1}, upgrade.Plan{Start: 3, End: 4, Version: 2}, upgrade.Plan{Start: 5, End: 6, Version: 4}}, true},
		{upgrade.Schedule{upgrade.Plan{Start: 1, End: 2, Version: 1}, upgrade.Plan{Start: 3, End: 4, Version: 2}, upgrade.Plan{Start: 5, End: 10, Version: 1}}, false},
	}

	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("case%d", idx), func(t *testing.T) {
			if tc.valid {
				require.NoError(t, tc.schedule.ValidateBasic())
			} else {
				require.Error(t, tc.schedule.ValidateBasic())
			}
		})
	}
}

func TestScheduleValidateVersions(t *testing.T) {
	testCases := []struct {
		schedule    upgrade.Schedule
		appVersions []uint64
		valid       bool
	}{
		// can be empty
		{upgrade.Schedule{}, []uint64{1, 2, 3}, true},
		{upgrade.Schedule{upgrade.Plan{Version: 3}}, []uint64{1, 2, 3}, true},
		{upgrade.Schedule{upgrade.Plan{Version: 4}}, []uint64{1, 2, 3}, false},
		{upgrade.Schedule{upgrade.Plan{Version: 2}, upgrade.Plan{Version: 5}}, []uint64{1, 2, 3}, false},
	}

	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("case%d", idx), func(t *testing.T) {
			if tc.valid {
				require.NoError(t, tc.schedule.ValidateVersions(tc.appVersions))
			} else {
				require.Error(t, tc.schedule.ValidateVersions(tc.appVersions))
			}
		})
	}
}
