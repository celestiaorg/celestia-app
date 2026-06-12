package networks

import (
	"strings"
	"testing"
)

func TestMochaConfigUpdate(t *testing.T) {
	config := NewMochaConfig()

	if !strings.Contains(config.Seeds, "14656") {
		t.Errorf("Expected seeds to use port 14656, got: %s", config.Seeds)
	}

	if !strings.HasPrefix(config.Seeds, "b402fe40") {
		t.Errorf("Expected seeds to start with b402fe40, got: %s", config.Seeds)
	}

	seedList := strings.Split(config.Seeds, ",")
	if len(seedList) < 2 {
		t.Errorf("Expected at least 2 seeds, got %d", len(seedList))
	}
}

// TestMochaConfigRPCsAreDistinct verifies that the mocha RPC list contains at
// least two distinct endpoints. CometBFT state sync requires >= 2 RPC servers
// to cross-verify the app hash header; listing the same host twice satisfies
// the count but provides no redundancy, so a single slow or unavailable
// provider stalls state sync (see flaky TestSyncToTipMocha nightly failures).
func TestMochaConfigRPCsAreDistinct(t *testing.T) {
	config := NewMochaConfig()

	if len(config.RPCs) < 2 {
		t.Fatalf("Expected at least 2 RPC servers for state sync, got %d", len(config.RPCs))
	}

	seen := make(map[string]bool)
	for _, rpc := range config.RPCs {
		if seen[rpc] {
			t.Errorf("Duplicate RPC endpoint %q: state sync needs distinct servers for redundancy", rpc)
		}
		seen[rpc] = true
	}

	if len(seen) < 2 {
		t.Errorf("Expected at least 2 distinct RPC endpoints, got %d distinct out of %d", len(seen), len(config.RPCs))
	}
}
