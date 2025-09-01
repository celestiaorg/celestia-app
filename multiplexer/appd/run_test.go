//go:build multiplexer

package appd

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/internal/embedding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateExecCommand execs a command to an embedded binary.
func TestCreateExecCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test which expects an embedded binary")
	}

	binaryGenerators := []func() (string, []byte, error){
		embedding.CelestiaAppV3,
		embedding.CelestiaAppV4,
		embedding.CelestiaAppV5,
	}

	for idx, binaryGenerator := range binaryGenerators {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			version, compressedBinary, err := binaryGenerator()
			require.NoError(t, err)

			appdInstance, err := New(version, compressedBinary)
			require.NoError(t, err)
			require.NotNil(t, appdInstance)

			cmd := appdInstance.CreateExecCommand("version")
			require.NotNil(t, cmd)

			var outputBuffer bytes.Buffer
			cmd.Stdout = &outputBuffer

			require.NoError(t, cmd.Run())

			want := strings.TrimPrefix(version, "v")
			got := outputBuffer.String()
			require.NotEmpty(t, got)
			assert.Contains(t, got, want)
		})
	}
}

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
