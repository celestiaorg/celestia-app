package appd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureBinaryDecompressed_ZipSlip(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte("malicious content")
	err := tw.WriteHeader(&tar.Header{
		Name: "../../../tmp/evil.txt",
		Mode: 0o644,
		Size: int64(len(content)),
	})
	require.NoError(t, err)
	_, err = tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	version := "v0.0.0-zipslip-test"
	defer os.RemoveAll(getDirectoryForVersion(version))

	err = ensureBinaryDecompressed(version, buf.Bytes())
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside target directory")
}

// TestEnsureBinaryDecompressed_FailedExtractionDoesNotPersist asserts that a
// failed extraction leaves no version directory behind. Otherwise a later
// start sees the directory, treats the version as already decompressed, and
// skips re-extracting the binary.
func TestEnsureBinaryDecompressed_FailedExtractionDoesNotPersist(t *testing.T) {
	version := "v0.0.0-failed-extraction-test"
	defer os.RemoveAll(getDirectoryForVersion(version))

	// A tar entry that escapes the target directory makes extraction fail
	// partway through, after the version directory has been created.
	var malicious bytes.Buffer
	gw := gzip.NewWriter(&malicious)
	tw := tar.NewWriter(gw)
	content := []byte("malicious content")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "../../../tmp/evil.txt",
		Mode: 0o644,
		Size: int64(len(content)),
	}))
	_, err := tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	require.Error(t, ensureBinaryDecompressed(version, malicious.Bytes()))
	require.False(t, isBinaryDecompressed(version), "failed extraction must not leave the version directory behind")

	// A retry with a valid archive must actually extract the binary.
	var valid bytes.Buffer
	gw = gzip.NewWriter(&valid)
	tw = tar.NewWriter(gw)
	binary := []byte("#!/bin/sh\necho hello\n")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "celestia-appd",
		Mode: 0o755,
		Size: int64(len(binary)),
	}))
	_, err = tw.Write(binary)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	require.NoError(t, ensureBinaryDecompressed(version, valid.Bytes()))
	got, err := os.ReadFile(filepath.Join(getDirectoryForVersion(version), "celestia-appd"))
	require.NoError(t, err)
	require.Equal(t, binary, got)
}

// TestEnsureBinaryDecompressed_Concurrent asserts that two instances sharing a
// node home can extract the same version at once. The loser of the publish
// race must accept the directory the winner published instead of failing.
func TestEnsureBinaryDecompressed_Concurrent(t *testing.T) {
	version := "v0.0.0-concurrent-test"
	defer os.RemoveAll(getDirectoryForVersion(version))

	var archive bytes.Buffer
	gw := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gw)
	binary := []byte("#!/bin/sh\necho hello\n")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "celestia-appd",
		Mode: 0o755,
		Size: int64(len(binary)),
	}))
	_, err := tw.Write(binary)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	const instances = 8
	start := make(chan struct{})
	errs := make(chan error, instances)
	var wg sync.WaitGroup
	for range instances {
		wg.Go(func() {
			<-start
			errs <- ensureBinaryDecompressed(version, archive.Bytes())
		})
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "concurrent extraction of the same version must succeed")
	}

	got, err := os.ReadFile(filepath.Join(getDirectoryForVersion(version), "celestia-appd"))
	require.NoError(t, err)
	require.Equal(t, binary, got)
}
