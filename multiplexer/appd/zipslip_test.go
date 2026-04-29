package appd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
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
