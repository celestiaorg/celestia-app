package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmdHomeFromEnv(t *testing.T) {
	t.Setenv(envHome, "/tmp/fibre-home")

	cmd := newRootCmd()

	homeFlag := cmd.PersistentFlags().Lookup(flagHome)
	require.NotNil(t, homeFlag)
	assert.Equal(t, "/tmp/fibre-home", homeFlag.Value.String())
}

func TestNewRootCmdHomeEmptyEnv(t *testing.T) {
	t.Setenv(envHome, "")

	cmd := newRootCmd()

	homeFlag := cmd.PersistentFlags().Lookup(flagHome)
	require.NotNil(t, homeFlag)
	assert.Equal(t, defaultHomePath(), homeFlag.Value.String())
}

func TestNewRootCmdHomeFlagDefaultDoesNotUseEnv(t *testing.T) {
	t.Setenv(envHome, "/tmp/fibre-home")

	cmd := newRootCmd()

	homeFlag := cmd.PersistentFlags().Lookup(flagHome)
	require.NotNil(t, homeFlag)
	assert.Equal(t, defaultHomePath(), homeFlag.DefValue)
	assert.Equal(t, "/tmp/fibre-home", homeFlag.Value.String())
}

// TestStartHome verifies the full CLI path: home flag resolution, config
// initialization, and config file creation via the root command.
// The server will fail to connect to the app gRPC endpoint (expected),
// but the home dir should be fully initialized before that.
func TestStartHome(t *testing.T) {
	home := t.TempDir()

	cmd := newRootCmd()
	cmd.SetArgs([]string{"start", "--" + flagHome, home})
	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err, "expected error: no app running")

	// config file was created at the expected path.
	configPath := fibre.DefaultConfigPath(home)
	assert.FileExists(t, configPath)
	assert.DirExists(t, filepath.Dir(configPath))

	// config file is loadable and has defaults.
	var cfg fibre.ServerConfig
	require.NoError(t, cfg.Load(configPath))

	defaults := fibre.DefaultServerConfig()
	assert.Equal(t, defaults.ServerListenAddress, cfg.ServerListenAddress)
	assert.Equal(t, defaults.AppGRPCAddress, cfg.AppGRPCAddress)
	assert.Equal(t, defaults.SignerListenAddress, cfg.SignerListenAddress)
}
