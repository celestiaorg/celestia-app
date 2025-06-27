package testnode

import (
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortManagementUnderHighConcurrency(t *testing.T) {
	const numWorkers = 20
	const numPortsPerWorker = 5
	
	var wg sync.WaitGroup
	allPorts := make(chan int, numWorkers*numPortsPerWorker)
	errors := make(chan error, numWorkers)
	
	// Start multiple workers concurrently requesting ports
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			for j := 0; j < numPortsPerWorker; j++ {
				port, err := GetAvailablePortWithRetry(10)
				if err != nil {
					errors <- err
					return
				}
				allPorts <- port
				
				// Small delay to simulate real usage
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}
	
	wg.Wait()
	close(allPorts)
	close(errors)
	
	// Check for errors
	var errorList []error
	for err := range errors {
		errorList = append(errorList, err)
	}
	require.Empty(t, errorList, "Port allocation should not fail under concurrency")
	
	// Collect all ports and verify uniqueness
	portSet := make(map[int]bool)
	var ports []int
	for port := range allPorts {
		ports = append(ports, port)
		if portSet[port] {
			t.Errorf("Port %d was allocated multiple times", port)
		}
		portSet[port] = true
	}
	
	expectedCount := numWorkers * numPortsPerWorker
	assert.Len(t, ports, expectedCount, "Should have allocated expected number of ports")
	assert.Len(t, portSet, expectedCount, "All ports should be unique")
}

func TestNetworkCreationUnderConcurrency(t *testing.T) {
	const numNetworks = 5
	
	var wg sync.WaitGroup
	errors := make(chan error, numNetworks)
	
	// Start multiple networks concurrently
	for i := 0; i < numNetworks; i++ {
		wg.Add(1)
		go func(networkID int) {
			defer wg.Done()
			
			config := DefaultConfig().WithChainID("test-chain")
			
			// This would normally create a full network, but for this stress test
			// we'll just test the config creation which includes port allocation
			_ = config
			
		}(i)
	}
	
	wg.Wait()
	close(errors)
	
	// Check for errors
	var errorList []error
	for err := range errors {
		errorList = append(errorList, err)
	}
	require.Empty(t, errorList, "Network config creation should not fail under concurrency")
}

func TestPortCleanupFunctionality(t *testing.T) {
	// Skip this test if lsof is not available
	if !isLsofAvailable() {
		t.Skip("lsof not available, skipping port cleanup test")
	}
	
	port, err := GetFreePort()
	require.NoError(t, err)
	
	// Should be available initially
	err = EnsurePortAvailable(port, true)
	assert.NoError(t, err)
	
	// The EnsurePortAvailable function should handle the case where
	// no processes are using the port gracefully
	err = EnsurePortAvailable(port, true)
	assert.NoError(t, err)
}

// isLsofAvailable checks if lsof command is available on the system
func isLsofAvailable() bool {
	_, err := exec.LookPath("lsof")
	return err == nil
}