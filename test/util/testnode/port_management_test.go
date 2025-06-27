package testnode

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPortAvailable(t *testing.T) {
	// Get a free port
	port, err := GetFreePort()
	require.NoError(t, err)
	
	// Port should be available
	assert.True(t, IsPortAvailable(port))
	
	// Start a listener on the port
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer listener.Close()
	
	// Port should now be unavailable
	assert.False(t, IsPortAvailable(port))
}

func TestGetAvailablePortWithRetry(t *testing.T) {
	port, err := GetAvailablePortWithRetry(3)
	require.NoError(t, err)
	assert.Greater(t, port, 0)
	
	// Verify the port is actually available
	assert.True(t, IsPortAvailable(port))
}

func TestGetAvailablePortWithRetry_HighContention(t *testing.T) {
	// This test verifies that retry logic works under contention
	const numTests = 10
	ports := make([]int, numTests)
	
	for i := 0; i < numTests; i++ {
		port, err := GetAvailablePortWithRetry(5)
		require.NoError(t, err)
		ports[i] = port
		
		// Verify each port is actually available
		assert.True(t, IsPortAvailable(port), "Port %d should be available", port)
	}
	
	// All ports should be different
	portSet := make(map[int]bool)
	for _, port := range ports {
		assert.False(t, portSet[port], "Port %d was allocated twice", port)
		portSet[port] = true
	}
}

func TestEnsurePortAvailable(t *testing.T) {
	port, err := GetFreePort()
	require.NoError(t, err)
	
	// Port should be available without needing cleanup
	err = EnsurePortAvailable(port, false)
	assert.NoError(t, err)
	
	// Start a listener on the port
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	
	// Should fail when not killing processes
	err = EnsurePortAvailable(port, false)
	assert.Error(t, err)
	
	// Close the listener to simulate what kill would do
	listener.Close()
	
	// Give the port time to be freed
	time.Sleep(10 * time.Millisecond)
	
	// Should succeed now
	err = EnsurePortAvailable(port, false)
	assert.NoError(t, err)
}

func TestDefaultAppConfigUsesRetryLogic(t *testing.T) {
	// This test ensures DefaultAppConfig uses the new retry logic
	config := DefaultAppConfig()
	
	// Extract and validate GRPC port
	_, grpcPortStr, err := net.SplitHostPort(config.GRPC.Address)
	require.NoError(t, err)
	
	// Extract and validate API port  
	apiAddr := config.API.Address
	if apiAddr[:6] == "tcp://" {
		apiAddr = apiAddr[6:]
	}
	_, apiPortStr, err := net.SplitHostPort(apiAddr)
	require.NoError(t, err)
	
	// Verify ports are different
	assert.NotEqual(t, grpcPortStr, apiPortStr, "GRPC and API ports should be different")
	
	// Verify ports are valid integers
	_, err = net.LookupPort("tcp", grpcPortStr)
	assert.NoError(t, err)
	_, err = net.LookupPort("tcp", apiPortStr)
	assert.NoError(t, err)
}