//go:build multiplexer

package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnsupportedFlags(t *testing.T) {
	// v6-v9 lack only the flags added in v10.
	require.Equal(t, map[string]bool{"otel-endpoint": true, "fibre-promise-cache": false}, unsupportedFlags(9))
	require.Equal(t, unsupportedFlags(9), unsupportedFlags(6))

	// v4 and v5 also lack flags added in v6.
	require.Contains(t, unsupportedFlags(5), "bypass-config-overrides")
	require.Contains(t, unsupportedFlags(4), "delayed-precommit-timeout")
	require.NotContains(t, unsupportedFlags(5), "with-comet")

	// v3 (cosmos-sdk v0.46) also lacks cosmos-sdk v0.50 flags.
	for _, appVersion := range []uint64{1, 2, 3} {
		flags := unsupportedFlags(appVersion)
		require.Contains(t, flags, "otel-endpoint")
		require.Contains(t, flags, "bypass-config-overrides")
		require.Contains(t, flags, "with-comet")
		require.Contains(t, flags, "log_no_color")
		require.True(t, flags["shutdown-grace"])
	}
}
