package testnode

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetDeterministicPort(t *testing.T) {
	// Test that we get sequential ports
	port1 := GetDeterministicPort()
	port2 := GetDeterministicPort()
	port3 := GetDeterministicPort()

	assert.Greater(t, port1, 20000, "Port should be greater than 20000")
	assert.Equal(t, port1+1, port2, "Ports should be sequential")
	assert.Equal(t, port2+1, port3, "Ports should be sequential")
}

func TestGetDeterministicPortConcurrent(t *testing.T) {
	// Test concurrent access to ensure no duplicate ports
	const numGoroutines = 50
	const portsPerGoroutine = 10
	
	var wg sync.WaitGroup
	portChannel := make(chan int, numGoroutines*portsPerGoroutine)
	
	// Start multiple goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < portsPerGoroutine; j++ {
				port := GetDeterministicPort()
				portChannel <- port
			}
		}()
	}
	
	wg.Wait()
	close(portChannel)
	
	// Collect all ports and check for duplicates
	portSet := make(map[int]bool)
	var ports []int
	for port := range portChannel {
		if portSet[port] {
			t.Errorf("Duplicate port detected: %d", port)
		}
		portSet[port] = true
		ports = append(ports, port)
	}
	
	// Should have exactly the expected number of unique ports
	expectedPorts := numGoroutines * portsPerGoroutine
	assert.Equal(t, expectedPorts, len(ports), "Should have correct number of ports")
	assert.Equal(t, expectedPorts, len(portSet), "Should have correct number of unique ports")
}