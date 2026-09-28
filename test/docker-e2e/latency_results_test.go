package docker_e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectLatencyResultsLogsFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  string
	}{
		{name: "empty results"},
		{name: "broadcast rejected", row: ",,true\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fixture := filepath.Join(dir, "results.csv")
			require.NoError(t, os.WriteFile(fixture, []byte("Latency (ms),Effective Latency (ms),Failed\n"+tc.row), 0o600))
			calls := filepath.Join(dir, "calls")
			// Exercise collection without a Docker daemon or live network.
			require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(`#!/bin/sh
echo "$1" >> "$DOCKER_TEST_CALLS"
case "$1" in
  cp) cp "$DOCKER_TEST_CSV" "$3" ;;
  logs) echo '[BROADCAST_FAILED] account does not exist' ;;
esac
`), 0o700))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("DOCKER_TEST_CSV", fixture)
			t.Setenv("DOCKER_TEST_CALLS", calls)
			result, err := new(CelestiaTestSuite).CollectLatencyResults(context.Background(), t, "monitor")
			if tc.row == "" {
				require.ErrorContains(t, err, "no transactions recorded")
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, result.FailureCount)
				require.Zero(t, result.SuccessRate)
			}
			commands, err := os.ReadFile(calls)
			require.NoError(t, err)
			require.Contains(t, strings.Split(string(commands), "\n"), "logs")
		})
	}
}
