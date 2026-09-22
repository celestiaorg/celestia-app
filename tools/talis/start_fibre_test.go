package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/stretchr/testify/require"
)

func TestFibreObjectStorageScript(t *testing.T) {
	home := t.TempDir()
	script := fibreObjectStorageScript(home, "my-bucket", "us-east-2", "", "talis-chain", 5*time.Minute)

	out, err := exec.Command("bash", "-c", script).CombinedOutput()
	require.NoError(t, err, string(out))

	hostname, err := os.Hostname()
	require.NoError(t, err)

	cfg := fibre.DefaultServerConfig()
	require.NoError(t, cfg.Load(filepath.Join(home, fibre.DefaultConfigFileName)))
	require.NoError(t, cfg.ObjectStorage.Validate())
	require.Equal(t, "object", cfg.StorageBackend)
	require.Equal(t, "https://s3.us-east-2.amazonaws.com", cfg.ObjectStorage.Endpoint)
	require.Equal(t, "my-bucket", cfg.ObjectStorage.Bucket)
	require.Equal(t, "us-east-2", cfg.ObjectStorage.Region)
	require.Equal(t, "talis-chain/"+hostname, cfg.ObjectStorage.Prefix)
	require.Equal(t, 5*time.Minute, cfg.ObjectStorage.RequestTimeout)
}

func TestFibreObjectStorageScriptCustomEndpoint(t *testing.T) {
	home := t.TempDir()
	script := fibreObjectStorageScript(home, "b", "auto", "https://example.r2.cloudflarestorage.com", "c", time.Minute)

	out, err := exec.Command("bash", "-c", script).CombinedOutput()
	require.NoError(t, err, string(out))

	cfg := fibre.DefaultServerConfig()
	require.NoError(t, cfg.Load(filepath.Join(home, fibre.DefaultConfigFileName)))
	require.Equal(t, "https://example.r2.cloudflarestorage.com", cfg.ObjectStorage.Endpoint)
}
