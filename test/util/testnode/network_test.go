package testnode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReassignListenPorts exercises every listen-port field that the retry
// path in NewNetworkWithRetry must re-roll after a port binding error. If any
// of these fields is missed, a transient collision on that port will recur on
// every retry and the test will fail after maxRetries attempts (see #7137).
func TestReassignListenPorts(t *testing.T) {
	cfg := DefaultConfig()

	before := struct {
		tmRPC     string
		tmP2P     string
		tmRPCGRPC string
		tmPrivVal string
		appGRPC   string
		appAPI    string
	}{
		tmRPC:     cfg.TmConfig.RPC.ListenAddress,
		tmP2P:     cfg.TmConfig.P2P.ListenAddress,
		tmRPCGRPC: cfg.TmConfig.RPC.GRPCListenAddress,
		tmPrivVal: cfg.TmConfig.PrivValidatorGRPCListenAddr,
		appGRPC:   cfg.AppConfig.GRPC.Address,
		appAPI:    cfg.AppConfig.API.Address,
	}

	reassignListenPorts(cfg)

	assert.NotEqual(t, before.tmRPC, cfg.TmConfig.RPC.ListenAddress, "TmConfig.RPC.ListenAddress should be reassigned")
	assert.NotEqual(t, before.tmP2P, cfg.TmConfig.P2P.ListenAddress, "TmConfig.P2P.ListenAddress should be reassigned")
	assert.NotEqual(t, before.tmRPCGRPC, cfg.TmConfig.RPC.GRPCListenAddress, "TmConfig.RPC.GRPCListenAddress should be reassigned")
	assert.NotEqual(t, before.tmPrivVal, cfg.TmConfig.PrivValidatorGRPCListenAddr, "TmConfig.PrivValidatorGRPCListenAddr should be reassigned")
	assert.NotEqual(t, before.appGRPC, cfg.AppConfig.GRPC.Address, "AppConfig.GRPC.Address should be reassigned")
	assert.NotEqual(t, before.appAPI, cfg.AppConfig.API.Address, "AppConfig.API.Address should be reassigned")
	assert.True(t, cfg.AppConfig.API.Enable, "AppConfig.API.Enable should remain true")
}
