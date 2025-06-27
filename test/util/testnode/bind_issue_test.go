package testnode

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSimulateOriginalBindIssue attempts to reproduce the original "bind: address already in use" scenario
func TestSimulateOriginalBindIssue(t *testing.T) {
	const numConcurrentServers = 10
	const iterations = 5
	
	for iter := 0; iter < iterations; iter++ {
		t.Run(fmt.Sprintf("iteration_%d", iter), func(t *testing.T) {
			var wg sync.WaitGroup
			errors := make(chan error, numConcurrentServers)
			
			// Simulate multiple testnode creations that could conflict on ports
			for i := 0; i < numConcurrentServers; i++ {
				wg.Add(1)
				go func(serverID int) {
					defer wg.Done()
					
					// This simulates the flow that was causing the original issue
					config := DefaultAppConfig()
					
					// Extract the port from GRPC address
					_, _, err := net.SplitHostPort(config.GRPC.Address)
					if err != nil {
						errors <- fmt.Errorf("failed to parse GRPC address: %w", err)
						return
					}
					
					// Try to bind to the allocated port (simulating StartGRPCServer)
					listener, err := net.Listen("tcp", config.GRPC.Address)
					if err != nil {
						errors <- fmt.Errorf("server %d failed to bind to %s: %w", serverID, config.GRPC.Address, err)
						return
					}
					defer listener.Close()
					
					// Hold the port briefly to simulate server operation
					time.Sleep(10 * time.Millisecond)
					
				}(i)
			}
			
			wg.Wait()
			close(errors)
			
			// Check for any bind errors
			var errorList []error
			for err := range errors {
				errorList = append(errorList, err)
			}
			
			require.Empty(t, errorList, "No servers should fail to bind to their allocated ports")
		})
	}
}

// TestRapidNetworkCreationDestruction tests the scenario where networks are created and destroyed rapidly
func TestRapidNetworkCreationDestruction(t *testing.T) {
	const numRounds = 20
	
	for round := 0; round < numRounds; round++ {
		t.Run(fmt.Sprintf("round_%d", round), func(t *testing.T) {
			// Create multiple configs in rapid succession
			configs := make([]*srvconfig.Config, 3)
			for i := range configs {
				configs[i] = DefaultAppConfig()
			}
			
			// Verify all configs have different ports
			grpcPorts := make(map[string]bool)
			apiPorts := make(map[string]bool)
			
			for i, config := range configs {
				_, grpcPort, err := net.SplitHostPort(config.GRPC.Address)
				require.NoError(t, err, "Config %d should have valid GRPC address", i)
				
				apiAddr := config.API.Address
				if apiAddr[:6] == "tcp://" {
					apiAddr = apiAddr[6:]
				}
				_, apiPort, err := net.SplitHostPort(apiAddr)
				require.NoError(t, err, "Config %d should have valid API address", i)
				
				assert.False(t, grpcPorts[grpcPort], "GRPC port %s should be unique (config %d)", grpcPort, i)
				assert.False(t, apiPorts[apiPort], "API port %s should be unique (config %d)", apiPort, i)
				
				grpcPorts[grpcPort] = true
				apiPorts[apiPort] = true
			}
		})
	}
}

// TestPortAllocationUnderMemoryPressure tests port allocation when system resources might be constrained
func TestPortAllocationUnderMemoryPressure(t *testing.T) {
	// Create many listeners to put pressure on port allocation
	const numListeners = 100
	var listeners []net.Listener
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()
	
	// Create many active listeners
	for i := 0; i < numListeners; i++ {
		port, err := GetAvailablePortWithRetry(3)
		require.NoError(t, err)
		
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		require.NoError(t, err)
		listeners = append(listeners, listener)
	}
	
	// Now try to allocate more ports - this should still work
	for i := 0; i < 10; i++ {
		port, err := GetAvailablePortWithRetry(5)
		require.NoError(t, err)
		assert.Greater(t, port, 0)
		
		// Verify the port is actually available
		assert.True(t, IsPortAvailable(port))
	}
}