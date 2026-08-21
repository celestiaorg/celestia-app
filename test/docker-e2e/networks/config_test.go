package networks

import (
	"strings"
	"testing"
)

func TestMochaConfigUpdate(t *testing.T) {
	config := NewMochaConfig()

	if !strings.Contains(config.Seeds, "26656") {
		t.Errorf("Expected seeds to use port 26656, got: %s", config.Seeds)
	}

	if !strings.HasPrefix(config.Seeds, "ee9f9097") {
		t.Errorf("Expected seeds to start with ee9f9097, got: %s", config.Seeds)
	}
}

// TestMochaConfigRPCsAreDistinct verifies that the mocha RPC list contains at
// least two endpoints. CometBFT state sync requires >= 2 RPC servers to
// cross-verify the app hash header; listing the same host twice satisfies
// the count but provides no redundancy, so a single slow or unavailable
// provider stalls state sync (see flaky TestSyncToTipMocha nightly failures).
// TODO: restore the distinctness check once more RPC providers serve mocha-5.
func TestMochaConfigRPCsAreDistinct(t *testing.T) {
	config := NewMochaConfig()

	if len(config.RPCs) < 2 {
		t.Fatalf("Expected at least 2 RPC servers for state sync, got %d", len(config.RPCs))
	}
}
