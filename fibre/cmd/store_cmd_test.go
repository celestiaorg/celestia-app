package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

func TestWriteObjectStorageCheck(t *testing.T) {
	result := fibre.ObjectStorageCheckResult{
		CanaryKey:     "fibre/.fibre-check/canary",
		HeadBucketErr: errors.New("access denied"),
	}
	var output bytes.Buffer
	require.NoError(t, writeObjectStorageCheck(&output, result))
	require.Regexp(t, `(?m)^head bucket\s+failed\s+access denied$`, output.String())
	require.Regexp(t, `(?m)^put canary\s+ok\s*$`, output.String())
	require.Regexp(t, `(?m)^delete canary\s+ok\s*$`, output.String())
	require.Contains(t, output.String(), result.CanaryKey)
}

func TestWriteObjectStorageCheckReportsIndependentDelete(t *testing.T) {
	result := fibre.ObjectStorageCheckResult{
		CanaryKey:       "fibre/.fibre-check/canary",
		PutCanaryErr:    errors.New("put denied"),
		DeleteCanaryErr: errors.New("delete denied"),
	}
	var output bytes.Buffer
	require.NoError(t, writeObjectStorageCheck(&output, result))
	require.Regexp(t, `(?m)^put canary\s+failed\s+put denied$`, output.String())
	require.Regexp(t, `(?m)^delete canary\s+failed\s+delete denied$`, output.String())
}

func TestStoreStatsCmdDoesNotInitializeHome(t *testing.T) {
	home := t.TempDir()
	db, err := pebbledb.Open(home, &pebbledb.Options{})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	cmd := newStoreStatsCmd()
	cmd.Flags().String(flagHome, home, "")
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Regexp(t, `(?m)^storage backend\s+local$`, output.String())
	require.Regexp(t, `(?m)^markers\s+0$`, output.String())
	require.NoDirExists(t, home+"/config")
}

func TestStoreStatsCmdIgnoresUnknownConfigFields(t *testing.T) {
	home := t.TempDir()
	db, err := pebbledb.Open(home, &pebbledb.Options{})
	require.NoError(t, err)
	require.NoError(t, db.Close())
	configPath := filepath.Join(home, "config", "server_config.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("storage_backend = \"object\"\nnewer_field = true\n"), 0o600))

	cmd := newStoreStatsCmd()
	cmd.Flags().String(flagHome, home, "")
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Regexp(t, `(?m)^storage backend\s+object$`, output.String())
}

func TestStoreMarkersCmdPrintsMalformedKey(t *testing.T) {
	home := t.TempDir()
	db, err := pebbledb.Open(home, &pebbledb.Options{})
	require.NoError(t, err)
	key := "/shard/malformed"
	require.NoError(t, db.Set([]byte(key), markerValue(1, 10), pebbledb.Sync))
	require.NoError(t, db.Close())

	cmd := newStoreMarkersCmd()
	cmd.Flags().String(flagHome, home, "")
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Contains(t, output.String(), "KEY")
	require.Contains(t, output.String(), key)
	require.Contains(t, output.String(), "invalid shard key")
}

func TestStoreMarkersCmdFiltersAndLimits(t *testing.T) {
	home := t.TempDir()
	db, err := pebbledb.Open(home, &pebbledb.Options{})
	require.NoError(t, err)
	commitment := strings.Repeat("01", 32)
	require.NoError(t, db.Set([]byte("/shard/"+commitment+"/01"), markerValue(1, 10), pebbledb.Sync))
	require.NoError(t, db.Set([]byte("/shard/"+commitment+"/02"), markerValue(2, 20), pebbledb.Sync))
	require.NoError(t, db.Set([]byte("/shard/"+commitment+"/03"), markerValue(2, 30), pebbledb.Sync))
	require.NoError(t, db.Close())

	cmd := newStoreMarkersCmd()
	cmd.Flags().String(flagHome, home, "")
	cmd.SetArgs([]string{"--backend", "object", "--limit", "1"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Contains(t, output.String(), "object")
	require.Contains(t, output.String(), "/shard/"+commitment+"/02")
	require.NotContains(t, output.String(), "/shard/"+commitment+"/01")
	require.NotContains(t, output.String(), "/shard/"+commitment+"/03")
	require.Contains(t, output.String(), "more exist, use --limit 0")

	db, err = pebbledb.Open(home, &pebbledb.Options{})
	require.NoError(t, err)
	require.NoError(t, db.Set([]byte("/shard/"+commitment+"/04"), []byte{1}, pebbledb.Sync))
	require.NoError(t, db.Close())
	cmd = newStoreMarkersCmd()
	cmd.Flags().String(flagHome, home, "")
	cmd.SetArgs([]string{"--backend", "invalid"})
	output.Reset()
	cmd.SetOut(&output)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Contains(t, output.String(), "/shard/"+commitment+"/04")
	require.NotContains(t, output.String(), "/shard/"+commitment+"/01")
}

func markerValue(backend byte, size uint64) []byte {
	// Mirrors fibre.encodeShardMarkerForBackend, which is package-private.
	marker := make([]byte, 10)
	marker[0] = 1
	marker[1] = backend
	binary.BigEndian.PutUint64(marker[2:], size)
	return marker
}
