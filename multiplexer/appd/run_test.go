package appd

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStart(t *testing.T) {
	t.Run("should start the process", func(t *testing.T) {
		mockBinary := createMockExecutable(t, "sleep 10")
		defer os.Remove(mockBinary) // Cleanup after test

		appdInstance := &Appd{
			path:   mockBinary,
			stdin:  os.Stdin,
			stdout: os.Stdout,
			stderr: os.Stderr,
		}

		require.True(t, appdInstance.IsStopped())
		require.False(t, appdInstance.IsRunning())

		err := appdInstance.Start()
		require.NoError(t, err)

		require.True(t, appdInstance.IsRunning())
		require.False(t, appdInstance.IsStopped())
	})
	t.Run("should return an error if the binary is non-existent", func(t *testing.T) {
		appdInstance := &Appd{
			path:   "/non/existent/binary",
			stdin:  os.Stdin,
			stdout: os.Stdout,
			stderr: os.Stderr,
		}

		err := appdInstance.Start()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to start")
		require.True(t, appdInstance.IsStopped())
		require.False(t, appdInstance.IsRunning())
	})
}

func TestStop(t *testing.T) {
	t.Run("should return no error if the process is not running", func(t *testing.T) {
		mockBinary := createMockExecutable(t, "sleep 10")
		defer os.Remove(mockBinary) // Cleanup after test

		appdInstance := &Appd{
			path:   mockBinary,
			stdin:  os.Stdin,
			stdout: os.Stdout,
			stderr: os.Stderr,
		}

		require.True(t, appdInstance.IsStopped())
		require.False(t, appdInstance.IsRunning())

		err := appdInstance.Stop()
		require.NoError(t, err)

		require.True(t, appdInstance.IsStopped())
		require.False(t, appdInstance.IsRunning())
	})
	t.Run("should stop the process", func(t *testing.T) {
		mockBinary := createMockExecutable(t, "sleep 10")
		defer os.Remove(mockBinary) // Cleanup after test

		appdInstance := &Appd{
			path:   mockBinary,
			stdin:  os.Stdin,
			stdout: os.Stdout,
			stderr: os.Stderr,
		}

		require.True(t, appdInstance.IsStopped())
		require.False(t, appdInstance.IsRunning())

		err := appdInstance.Start()
		require.NoError(t, err)

		require.True(t, appdInstance.IsRunning())
		require.False(t, appdInstance.IsStopped())

		err = appdInstance.Stop()
		require.NoError(t, err)

		require.True(t, appdInstance.IsStopped())
		require.False(t, appdInstance.IsRunning())
	})
}

func TestWaitCh(t *testing.T) {
	t.Run("closes and reports no error when the process exits cleanly", func(t *testing.T) {
		mockBinary := createMockExecutable(t, "exit 0")
		defer os.Remove(mockBinary)

		appdInstance := &Appd{
			path:   mockBinary,
			stdin:  os.Stdin,
			stdout: os.Stdout,
			stderr: os.Stderr,
		}

		require.NoError(t, appdInstance.Start())

		select {
		case <-appdInstance.WaitCh():
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the process to exit")
		}

		require.NoError(t, appdInstance.ExitError())
		require.True(t, appdInstance.IsStopped())
	})

	t.Run("closes and reports an error when the process fails", func(t *testing.T) {
		mockBinary := createMockExecutable(t, "exit 3")
		defer os.Remove(mockBinary)

		appdInstance := &Appd{
			path:   mockBinary,
			stdin:  os.Stdin,
			stdout: os.Stdout,
			stderr: os.Stderr,
		}

		require.NoError(t, appdInstance.Start())

		select {
		case <-appdInstance.WaitCh():
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the process to exit")
		}

		require.Error(t, appdInstance.ExitError())
		require.True(t, appdInstance.IsStopped())
	})
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
	require.NoError(t, os.Chmod(tmpFile.Name(), 0o755))

	return tmpFile.Name()
}
