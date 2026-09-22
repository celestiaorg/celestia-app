package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigSaveTruncatesShorterConfig(t *testing.T) {
	dir := t.TempDir()
	long := NewConfig("experiment", "chain", AWS)
	long.AWSInstanceProfile = "a-very-long-instance-profile-name-that-makes-the-file-longer"
	require.NoError(t, long.Save(dir))

	short := NewConfig("experiment", "chain", AWS)
	require.NoError(t, short.Save(dir))

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	require.NoError(t, err)
	var got Config
	require.NoError(t, json.Unmarshal(data, &got), "config.json must not keep bytes from a longer previous save")
	require.Empty(t, got.AWSInstanceProfile)
}
