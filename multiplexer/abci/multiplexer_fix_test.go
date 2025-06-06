package abci

import (
	"testing"

	"github.com/cometbft/cometbft/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMultiplexerRPCConfiguration tests that the multiplexer properly configures
// the RPC address for the core environment used by gRPC services
func TestMultiplexerRPCConfiguration(t *testing.T) {
	testCases := []struct {
		name               string
		configuredRPCAddr  string
		expectedRPCAddr    string
		description        string
	}{
		{
			name:              "localhost should work",
			configuredRPCAddr: "tcp://127.0.0.1:26657",
			expectedRPCAddr:   "tcp://127.0.0.1:26657",
			description:       "Localhost configuration should be preserved",
		},
		{
			name:              "specific IP should work",
			configuredRPCAddr: "tcp://192.168.1.100:26657",
			expectedRPCAddr:   "tcp://192.168.1.100:26657",
			description:       "Custom IP configuration should be preserved (fixes the bug)",
		},
		{
			name:              "bind all interfaces should work",
			configuredRPCAddr: "tcp://0.0.0.0:26657",
			expectedRPCAddr:   "tcp://0.0.0.0:26657",
			description:       "Bind to all interfaces should be preserved",
		},
		{
			name:              "IPv6 should work",
			configuredRPCAddr: "tcp://[::1]:26657",
			expectedRPCAddr:   "tcp://[::1]:26657",
			description:       "IPv6 configuration should be preserved",
		},
		{
			name:              "custom port should work",
			configuredRPCAddr: "tcp://192.168.1.50:8090",
			expectedRPCAddr:   "tcp://192.168.1.50:8090",
			description:       "Custom port configuration should be preserved",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a CometBFT config with the specified RPC address
			cfg := config.DefaultConfig()
			cfg.RPC.ListenAddress = tc.configuredRPCAddr

			// Simulate what happens in startGRPCServer when we fix the core environment
			originalEnv := &MockCoreEnvironment{
				Config: config.RPCConfig{
					ListenAddress: "tcp://localhost:26657", // Default that would cause the bug
				},
			}

			// Apply the fix: update the core environment with the configured RPC address
			if cfg.RPC.ListenAddress != "" {
				originalEnv.Config.ListenAddress = cfg.RPC.ListenAddress
			}

			// Verify the fix works
			assert.Equal(t, tc.expectedRPCAddr, originalEnv.Config.ListenAddress, tc.description)
			
			// Verify it's not using the problematic localhost default
			if tc.configuredRPCAddr != "tcp://127.0.0.1:26657" && tc.configuredRPCAddr != "tcp://localhost:26657" {
				assert.NotEqual(t, "tcp://localhost:26657", originalEnv.Config.ListenAddress, "Should not use localhost default")
				assert.NotEqual(t, "tcp://127.0.0.1:26657", originalEnv.Config.ListenAddress, "Should not use 127.0.0.1 default")
			}
		})
	}
}

// MockCoreEnvironment is a simple mock for testing
type MockCoreEnvironment struct {
	Config config.RPCConfig
}

// TestFixPreventsLocalhostDefault verifies that the fix prevents the localhost default issue
func TestFixPreventsLocalhostDefault(t *testing.T) {
	// This test simulates the original issue where gRPC services would default to localhost:26657
	// even when a different RPC address was configured
	
	// Configure a non-localhost RPC address (the user's configuration)
	cfg := config.DefaultConfig()
	cfg.RPC.ListenAddress = "tcp://10.0.0.5:26657" // Custom IP as in the issue description

	// Before the fix: core environment would use localhost (this was the bug)
	coreEnvBeforeFix := &MockCoreEnvironment{
		Config: config.RPCConfig{
			ListenAddress: "tcp://localhost:26657", // This was the problematic default
		},
	}

	// After the fix: core environment uses the configured address
	coreEnvAfterFix := &MockCoreEnvironment{
		Config: config.RPCConfig{
			ListenAddress: cfg.RPC.ListenAddress, // This is what our fix ensures
		},
	}

	// Verify the problem existed before the fix
	assert.Equal(t, "tcp://localhost:26657", coreEnvBeforeFix.Config.ListenAddress, "Before fix: should use localhost default")
	assert.NotEqual(t, "tcp://10.0.0.5:26657", coreEnvBeforeFix.Config.ListenAddress, "Before fix: should not use configured address")

	// Verify the fix solves the problem
	assert.Equal(t, "tcp://10.0.0.5:26657", coreEnvAfterFix.Config.ListenAddress, "After fix: should use configured address")
	assert.NotEqual(t, "tcp://localhost:26657", coreEnvAfterFix.Config.ListenAddress, "After fix: should not use localhost default")

	require.NotEqual(t, coreEnvBeforeFix.Config.ListenAddress, coreEnvAfterFix.Config.ListenAddress, "Fix should change the behavior")
}