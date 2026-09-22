package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunRejectsNetworkConfigBeforeStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network.json")
	require.ErrorContains(t, run(config{networkConfig: path}), "load network config")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))
	require.ErrorContains(t, run(config{networkConfig: path}), "load network config")
}
