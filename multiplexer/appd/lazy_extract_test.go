package appd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// setNodeHome points nodeHome at dir for the duration of the test.
func setNodeHome(t *testing.T, dir string) {
	t.Helper()
	original := nodeHome
	nodeHome = dir
	t.Cleanup(func() { nodeHome = original })
}

// compressScript returns a gzipped tarball containing an executable script.
func compressScript(t *testing.T, command string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	script := []byte("#!/bin/sh\n" + command + "\n")
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "celestia-appd", Mode: 0o755, Size: int64(len(script))}))
	_, err := tw.Write(script)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func TestNewDoesNotExtract(t *testing.T) {
	setNodeHome(t, t.TempDir())

	a, err := New("v0.0.0-lazy", compressScript(t, "echo hello"))
	require.NoError(t, err)
	require.NoDirExists(t, getDirectoryForVersion("v0.0.0-lazy"))

	cmd, err := a.CreateExecCommand()
	require.NoError(t, err)
	var out bytes.Buffer
	cmd.Stdout = &out
	require.NoError(t, cmd.Run())
	require.Equal(t, "hello\n", out.String())
	require.DirExists(t, getDirectoryForVersion("v0.0.0-lazy"))
}

func TestExtractionErrorNamesDirectory(t *testing.T) {
	// A regular file as the node home makes extraction fail, even as root.
	home := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.WriteFile(home, nil, 0o600))
	setNodeHome(t, home)

	a, err := New("v0.0.0-unwritable", compressScript(t, "true"))
	require.NoError(t, err)

	_, err = a.CreateExecCommand()
	require.ErrorContains(t, err, filepath.Join(home, "bin", "v0.0.0-unwritable"))

	err = a.Start()
	require.ErrorContains(t, err, filepath.Join(home, "bin", "v0.0.0-unwritable"))
}
