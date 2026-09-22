package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testNetworkJSON = `{"source_ips":["10.0.0.10","10.0.1.10"],"validators":{"0123456789ABCDEF0123456789ABCDEF01234567":["10.0.0.20:7980","10.0.1.20:7980"]}}`

func TestStageFibreNetworkConfigs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "scp.log")
	t.Setenv("FIBRE_SCP_LOG", logPath)
	// Capture argv without accessing a remote host.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scp"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$FIBRE_SCP_LOG\"\n"), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	configDir := filepath.Join(dir, "configs with spaces")
	require.NoError(t, os.Mkdir(configDir, 0o700))
	hosts := []Instance{{Name: "validator-0", PublicIP: "192.0.2.1"}, {Name: "validator-1", PublicIP: "192.0.2.2"}}
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "validator-0.json"), []byte(testNetworkJSON), 0o600))
	_, err := stageFibreNetworkConfigs(context.Background(), configDir, hosts, "key with spaces", FibreTxSimSessionName)
	require.ErrorContains(t, err, "validator-1")
	_, err = os.Stat(logPath)
	require.True(t, os.IsNotExist(err), "must validate all hosts before SCP")
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "validator-1.json"), []byte("{}"), 0o600))
	_, err = fibreNetworkFiles(configDir, hosts)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "validator-1.json"), []byte(testNetworkJSON), 0o600))
	for _, session := range []string{FibreTxSimSessionName, FibreReaderSessionName} {
		arg, err := stageFibreNetworkConfigs(context.Background(), configDir, hosts, "key with spaces", session)
		require.NoError(t, err)
		require.Equal(t, " --network-config '/root/talis-"+session+"-network.json'", arg)
	}
	logged, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(logged), "key with spaces\n")
	require.Contains(t, string(logged), filepath.Join(configDir, "validator-0.json")+"\n")
	require.Equal(t, 4, strings.Count(string(logged), "root@192.0.2."))
	_, err = fibreNetworkFiles(configDir, []Instance{{Name: "../escape"}})
	require.Error(t, err)
	arg, err := stageFibreNetworkConfigs(context.Background(), "", hosts, "", FibreTxSimSessionName)
	require.NoError(t, err)
	require.Empty(t, arg)
}

func TestFibreServerLimitArgs(t *testing.T) {
	cmd := startFibreCmd()
	args, err := fibreServerLimitArgs(cmd)
	require.NoError(t, err)
	require.Empty(t, args)
	require.NoError(t, cmd.Flags().Set("max-connections", "128"))
	require.NoError(t, cmd.Flags().Set("max-concurrent-streams", "4"))
	args, err = fibreServerLimitArgs(cmd)
	require.NoError(t, err)
	require.Equal(t, " --max-connections 128 --max-concurrent-streams 4", args)
	require.NoError(t, cmd.Flags().Set("max-connections", "0"))
	_, err = fibreServerLimitArgs(cmd)
	require.Error(t, err)
}
