package appd

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestExitedDeliversFailure verifies that when an embedded process exits with
// a non-zero status on its own, Exited delivers the exit error and the Appd
// reports itself stopped.
func TestExitedDeliversFailure(t *testing.T) {
	bin := writeMockExecutable(t, "exit 1")
	a := &Appd{path: bin, stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}

	require.NoError(t, a.Start())

	select {
	case err := <-a.Exited():
		require.Error(t, err, "a non-zero exit should be delivered as an error")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the process exit to be delivered")
	}
	require.True(t, a.IsStopped())
}

// TestExitedDeliversCleanExit verifies that a process which exits cleanly on
// its own delivers a nil error on Exited.
func TestExitedDeliversCleanExit(t *testing.T) {
	bin := writeMockExecutable(t, "exit 0")
	a := &Appd{path: bin, stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}

	require.NoError(t, a.Start())

	select {
	case err := <-a.Exited():
		require.NoError(t, err, "a clean exit should deliver a nil error")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the process exit to be delivered")
	}
	require.True(t, a.IsStopped())
}

// TestExitedFiresAfterStop verifies that Stop terminates the process and that
// Exited fires afterwards, so a watcher blocked on it always unblocks.
func TestExitedFiresAfterStop(t *testing.T) {
	bin := writeMockExecutable(t, "sleep 10")
	a := &Appd{path: bin, stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}

	require.NoError(t, a.Start())
	require.True(t, a.IsRunning())

	require.NoError(t, a.Stop())
	require.True(t, a.IsStopped())

	select {
	case <-a.Exited():
	case <-time.After(2 * time.Second):
		t.Fatal("Exited should fire after Stop")
	}
}

// TestExitedNilBeforeStart verifies that Exited returns a nil channel (which
// never delivers) when the process was never started.
func TestExitedNilBeforeStart(t *testing.T) {
	a := &Appd{path: "/non/existent/binary", stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
	require.Nil(t, a.Exited())
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
