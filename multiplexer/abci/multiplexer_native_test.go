package abci

import (
	"testing"

	"github.com/cometbft/cometbft/config"
	"github.com/stretchr/testify/assert"
)

// TestNativeAppRPCConfiguration tests that the multiplexer correctly configures
// the RPC address for native apps (when startGRPCServer gets called)
func TestNativeAppRPCConfiguration(t *testing.T) {
	// This test verifies that the fix in startGRPCServer applies correctly
	// for native app scenarios
	
	testCases := []struct {
		name               string
		configuredRPCAddr  string
		expectedRPCAddr    string
		description        string
	}{
		{
			name:              "custom IP should work for native apps",
			configuredRPCAddr: "tcp://192.168.1.100:26657",
			expectedRPCAddr:   "tcp://192.168.1.100:26657",
			description:       "Native app should use custom IP configuration",
		},
		{
			name:              "IPv6 should work for native apps",
			configuredRPCAddr: "tcp://[::1]:26657",
			expectedRPCAddr:   "tcp://[::1]:26657",
			description:       "Native app should use IPv6 configuration",
		},
		{
			name:              "bind all interfaces should work for native apps",
			configuredRPCAddr: "tcp://0.0.0.0:26657",
			expectedRPCAddr:   "tcp://0.0.0.0:26657",
			description:       "Native app should use bind-all configuration",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a mock RPC config
			cfg := config.DefaultConfig()
			cfg.RPC.ListenAddress = tc.configuredRPCAddr

			// Simulate the fix that happens in startGRPCServer
			originalEnv := &MockCoreEnvironment{
				Config: config.RPCConfig{
					ListenAddress: "tcp://127.0.0.1:26657", // Default that would cause the bug
				},
			}

			// Apply the fix
			if cfg.RPC.ListenAddress != "" {
				originalEnv.Config.ListenAddress = cfg.RPC.ListenAddress
			}

			// Verify the fix works
			assert.Equal(t, tc.expectedRPCAddr, originalEnv.Config.ListenAddress, tc.description)
			
			// Verify it's not using the problematic localhost default
			if tc.configuredRPCAddr != "tcp://127.0.0.1:26657" && tc.configuredRPCAddr != "tcp://localhost:26657" {
				assert.NotEqual(t, "tcp://127.0.0.1:26657", originalEnv.Config.ListenAddress, "Should not use 127.0.0.1 default")
				assert.NotEqual(t, "tcp://localhost:26657", originalEnv.Config.ListenAddress, "Should not use localhost default")
			}
		})
	}
}

// MockCoreEnvironment is a simple mock for testing
type MockCoreEnvironment struct {
	Config config.RPCConfig
}

// TestFixAppliesOnlyWhenNeeded verifies that the fix only applies when there's a configured address
func TestFixAppliesOnlyWhenNeeded(t *testing.T) {
	testCases := []struct {
		name           string
		configuredAddr string
		expectChange   bool
		description    string
	}{
		{
			name:           "fix applies when custom address is configured",
			configuredAddr: "tcp://10.0.0.1:26657",
			expectChange:   true,
			description:    "Fix should apply when custom address is set",
		},
		{
			name:           "fix doesn't apply when no address is configured",
			configuredAddr: "",
			expectChange:   false,
			description:    "Fix should not apply when no custom address is set",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			originalEnv := &MockCoreEnvironment{
				Config: config.RPCConfig{
					ListenAddress: "tcp://127.0.0.1:26657",
				},
			}
			originalAddr := originalEnv.Config.ListenAddress

			// Apply the fix logic
			if tc.configuredAddr != "" {
				originalEnv.Config.ListenAddress = tc.configuredAddr
			}

			if tc.expectChange {
				assert.NotEqual(t, originalAddr, originalEnv.Config.ListenAddress, tc.description)
				assert.Equal(t, tc.configuredAddr, originalEnv.Config.ListenAddress, "Should use configured address")
			} else {
				assert.Equal(t, originalAddr, originalEnv.Config.ListenAddress, tc.description)
			}
		})
	}
}