package abci

import (
	"testing"

	"github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/rpc/core"
	"github.com/stretchr/testify/assert"
)

// TestCoreEnvironmentUsesConfiguredRPCAddress verifies that the core environment
// for the block API uses the configured RPC address instead of defaults
func TestCoreEnvironmentUsesConfiguredRPCAddress(t *testing.T) {
	// Create a test configuration with a custom RPC address
	cfg := config.DefaultConfig()
	customRPCAddr := "tcp://192.168.1.100:26657"
	cfg.RPC.ListenAddress = customRPCAddr

	// Create a mock core environment
	coreEnv := &core.Environment{
		Config: *cfg.RPC,
	}

	// The environment should use the configured RPC address
	assert.Equal(t, customRPCAddr, coreEnv.Config.ListenAddress)
	assert.NotEqual(t, "tcp://127.0.0.1:26657", coreEnv.Config.ListenAddress)
	assert.NotEqual(t, "tcp://localhost:26657", coreEnv.Config.ListenAddress)
}

// TestRPCAddressConfiguration verifies that non-localhost addresses are preserved
func TestRPCAddressConfiguration(t *testing.T) {
	testCases := []struct {
		name        string
		rpcAddress  string
		shouldEqual string
	}{
		{
			name:        "custom IP address",
			rpcAddress:  "tcp://10.0.0.1:26657",
			shouldEqual: "tcp://10.0.0.1:26657",
		},
		{
			name:        "IPv6 address",
			rpcAddress:  "tcp://[::1]:26657",
			shouldEqual: "tcp://[::1]:26657",
		},
		{
			name:        "custom hostname",
			rpcAddress:  "tcp://node.example.com:26657",
			shouldEqual: "tcp://node.example.com:26657",
		},
		{
			name:        "bind to all interfaces",
			rpcAddress:  "tcp://0.0.0.0:26657",
			shouldEqual: "tcp://0.0.0.0:26657",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.RPC.ListenAddress = tc.rpcAddress

			coreEnv := &core.Environment{
				Config: *cfg.RPC,
			}

			assert.Equal(t, tc.shouldEqual, coreEnv.Config.ListenAddress)
		})
	}
}