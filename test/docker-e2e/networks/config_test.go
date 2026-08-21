package networks

import (
	"strings"
	"testing"
)

func TestMochaConfigUpdate(t *testing.T) {
	config := NewMochaConfig()
<<<<<<< HEAD
	
	// Verify seeds configuration
	if !strings.Contains(config.Seeds, "14656") {
		t.Errorf("Expected seeds to use port 14656, got: %s", config.Seeds)
	}
	
	if !strings.HasPrefix(config.Seeds, "b402fe40") {
		t.Errorf("Expected seeds to start with b402fe40, got: %s", config.Seeds)
	}
	
	// Verify peers configuration
	if config.Peers == "" {
		t.Error("Expected peers to be configured, got empty string")
	}
	
	peerList := strings.Split(config.Peers, ",")
	if len(peerList) != 14 {
		t.Errorf("Expected 14 peers, got %d", len(peerList))
	}
	
	// Verify at least one known peer
	hasItrocketPeer := false
	for _, peer := range peerList {
		if strings.Contains(peer, "celestia-testnet-peer.itrocket.net:11656") {
			hasItrocketPeer = true
			break
		}
	}
	
	if !hasItrocketPeer {
		t.Error("Expected to find itrocket peer in peer list")
	}
	
	t.Logf("Mocha config updated successfully with %d peers and correct seeds", len(peerList))
=======

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
>>>>>>> 3d1ddece (chore!: replace mocha-4 with mocha-5 (#7709))
}
