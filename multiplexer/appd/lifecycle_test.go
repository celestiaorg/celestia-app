package appd

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestWait verifies that Wait reports unexpected process exits but stays quiet
// for intentional shutdowns. This is what lets the multiplexer notice that an
// embedded binary died and exit instead of hanging silently.
func TestWait(t *testing.T) {
	t.Run("returns nil if the process was never started", func(t *testing.T) {
		appdInstance := &Appd{
			path:   "/non/existent/binary",
			stdin:  os.Stdin,
			stdout: os.Stdout,
			stderr: os.Stderr,
		}

		require.NoError(t, appdInstance.Wait())
	})

	t.Run("returns an error when the process exits unexpectedly with a non-zero code", func(t *testing.T) {
		mockBinary := createMockBinary(t, "exit 1")

		appdInstance := newTestAppd(mockBinary)
		require.NoError(t, appdInstance.Start())

		err := appdInstance.Wait()
		require.Error(t, err)
		require.True(t, appdInstance.IsStopped())
	})

	t.Run("returns an error when the process exits unexpectedly with a zero code", func(t *testing.T) {
		mockBinary := createMockBinary(t, "exit 0")

		appdInstance := newTestAppd(mockBinary)
		require.NoError(t, appdInstance.Start())

		err := appdInstance.Wait()
		require.Error(t, err)
		require.True(t, appdInstance.IsStopped())
	})

	t.Run("returns nil when the process is stopped intentionally", func(t *testing.T) {
		mockBinary := createMockBinary(t, "sleep 10")

		appdInstance := newTestAppd(mockBinary)
		require.NoError(t, appdInstance.Start())
		require.True(t, appdInstance.IsRunning())

		require.NoError(t, appdInstance.Stop())

		require.NoError(t, appdInstance.Wait())
		require.True(t, appdInstance.IsStopped())
	})
}

// TestWaitUnblocksOnCrash ensures Wait returns promptly once the process dies
// rather than blocking forever.
func TestWaitUnblocksOnCrash(t *testing.T) {
	mockBinary := createMockBinary(t, "exit 1")

	appdInstance := newTestAppd(mockBinary)
	require.NoError(t, appdInstance.Start())

	done := make(chan error, 1)
	go func() {
		done <- appdInstance.Wait()
	}()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after the process exited")
	}
}

func newTestAppd(path string) *Appd {
	return &Appd{
		path:   path,
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
}

// createMockBinary creates a temporary executable shell script for tests.
func createMockBinary(t *testing.T, bashCommand string) string {
	t.Helper()

	tmpFile, err := os.CreateTemp(t.TempDir(), "mock-binary-*")
	require.NoError(t, err)

	_, err = tmpFile.WriteString("#!/bin/sh\n" + bashCommand + "\n")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())
	require.NoError(t, os.Chmod(tmpFile.Name(), 0o755))

	return tmpFile.Name()
}
