package appd

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestExitErrorOnFailure verifies that when an embedded process exits with a
// non-zero status on its own, the Appd reports it as stopped, records the
// exit error, and reports that the stop was not operator-initiated.
func TestExitErrorOnFailure(t *testing.T) {
	bin := writeMockExecutable(t, "exit 1")
	a := &Appd{path: bin, stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}

	require.NoError(t, a.Start())
	require.Eventually(t, a.IsStopped, 2*time.Second, 10*time.Millisecond, "process should exit on its own")
	require.False(t, a.StopInitiated(), "exit was not operator-initiated")
	require.Error(t, a.ExitError(), "a non-zero exit should be reported as an error")
}

// TestExitErrorOnCleanExit verifies that a process which exits cleanly on its
// own reports a nil exit error.
func TestExitErrorOnCleanExit(t *testing.T) {
	bin := writeMockExecutable(t, "exit 0")
	a := &Appd{path: bin, stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}

	require.NoError(t, a.Start())
	require.Eventually(t, a.IsStopped, 2*time.Second, 10*time.Millisecond, "process should exit on its own")
	require.False(t, a.StopInitiated())
	require.NoError(t, a.ExitError())
}

// TestStopInitiated verifies that StopInitiated reports false until Stop is
// called and true afterwards.
func TestStopInitiated(t *testing.T) {
	bin := writeMockExecutable(t, "sleep 10")
	a := &Appd{path: bin, stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}

	require.NoError(t, a.Start())
	require.False(t, a.StopInitiated(), "stop has not been requested yet")

	require.NoError(t, a.Stop())
	require.True(t, a.StopInitiated(), "stop has been requested")
	require.True(t, a.IsStopped())
}

// writeMockExecutable creates a temporary executable shell script that runs the
// provided command and returns its path.
func writeMockExecutable(t *testing.T, command string) string {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "mock-binary-*")
	require.NoError(t, err)
	_, err = f.WriteString("#!/bin/sh\n" + command + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Chmod(f.Name(), 0o755))

	return f.Name()
}
