package appd

import (
	"bytes"
	"os"
	"testing"

	"github.com/01builders/nova/appd/internal/testdata"
	"github.com/stretchr/testify/require"
)

// TestCreateExecCommand execs a command to an embedded binary.
func TestCreateExecCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test which expects an embedded binary")
	}

	bin, err := testdata.CelestiaApp()
	require.NoError(t, err)

	appdInstance, err := New("test-app", bin)
	require.NoError(t, err)
	require.NotNil(t, appdInstance)

	cmd := appdInstance.CreateExecCommand("version")
	require.NotNil(t, cmd)

	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer

	require.NoError(t, cmd.Run())

	output := outputBuffer.String()
	require.NotEmpty(t, output)
}

// TestStart_Success ensures that the provided executable is launched and provided a pid.
// and that the pid is reset once the process exits.
func TestStart_Success(t *testing.T) {
	mockBinary := createMockExecutable(t, "sleep 1")
	defer os.Remove(mockBinary) // Cleanup after test

	appdInstance := &Appd{
		path:   mockBinary,
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}

	// Start the process
	err := appdInstance.Start()
	require.NoError(t, err, "Start should not return an error")

	// Ensure PID is set
	require.Greater(t, appdInstance.Pid(), 0, "Process PID should be greater than 0")

	// Stop the process after the test
	err = appdInstance.Stop()
	require.NoError(t, err, "Stop should terminate the process")
}

// TestStart_InvalidBinary ensures that the appd instance errors out if the binary does not exist.
func TestStart_InvalidBinary(t *testing.T) {
	appdInstance := &Appd{
		path:   "/non/existent/binary",
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		pid:    AppdStopped,
	}

	// Start should return an error
	err := appdInstance.Start()
	require.Error(t, err, "Expected an error when starting a non-existent binary")
	require.Contains(t, err.Error(), "failed to start", "Error message should contain failure reason")

	require.Equal(t, AppdStopped, appdInstance.Pid(), "PID should remain AppdStopped")
}

// createMockExecutable creates a temporary mock binary that can be executed in tests.
func createMockExecutable(t *testing.T, bashCommand string) string {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "mock-binary-*")
	require.NoError(t, err)

	// Write a simple script that sleeps for a short time to simulate execution
	_, err = tmpFile.WriteString("#!/bin/sh\n" + bashCommand + "\n")
	require.NoError(t, err)

	// Close the file before setting it as executable
	require.NoError(t, tmpFile.Close())

	// Make it executable
	require.NoError(t, os.Chmod(tmpFile.Name(), 0755))

	return tmpFile.Name()
}
