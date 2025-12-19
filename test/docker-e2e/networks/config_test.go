package networks

import (
	"strings"
	"testing"
)

func TestMochaConfigUpdate(t *testing.T) {
	config := NewMochaConfig()
	
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
}
