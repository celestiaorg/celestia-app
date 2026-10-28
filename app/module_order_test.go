package app_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestModuleOrder pins the order in which the module manager runs block and
// genesis hooks. Changing any order alters state transitions, so it must be a
// deliberate, version-gated decision.
func TestModuleOrder(t *testing.T) {
	mm := getTestApp().ModuleManager

	require.Equal(t, []string{"upgrade"}, mm.OrderPreBlockers)
	require.Equal(t, []string{
		"capability",
		"mint",
		"distribution",
		"staking",
		"slashing",
		"evidence",
		"ibc",
		"transfer",
		"genutil",
		"blob",
		"params",
		"authz",
		"signal",
		"minfee",
		"interchainaccounts",
		"packetfowardmiddleware",
		"zkism",
		"fibre",
		"valaddr",
	}, mm.OrderBeginBlockers)
	require.Equal(t, []string{
		"gov",
		"staking",
		"capability",
		"mint",
		"distribution",
		"slashing",
		"evidence",
		"ibc",
		"transfer",
		"feegrant",
		"auth",
		"bank",
		"genutil",
		"blob",
		"params",
		"authz",
		"vesting",
		"signal",
		"minfee",
		"packetfowardmiddleware",
		"interchainaccounts",
		"fibre",
		"valaddr",
	}, mm.OrderEndBlockers)

	initGenesis := []string{
		"capability",
		"consensus",
		"auth",
		"bank",
		"distribution",
		"staking",
		"slashing",
		"gov",
		"mint",
		"ibc",
		"minfee",
		"genutil",
		"evidence",
		"transfer",
		"blob",
		"vesting",
		"feegrant",
		"params",
		"authz",
		"signal",
		"packetfowardmiddleware",
		"interchainaccounts",
		"upgrade",
		"circuit",
		"hyperlane",
		"warp",
		"zkism",
		"forwarding",
		"fibre",
		"valaddr",
	}
	require.Equal(t, initGenesis, mm.OrderInitGenesis)
	require.Equal(t, initGenesis, mm.OrderExportGenesis)
}
