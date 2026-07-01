package fibre

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerConfigLoadNoFile(t *testing.T) {
	cfg := DefaultServerConfig()
	err := cfg.Load("/nonexistent/path/config.toml")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:7980", cfg.ServerListenAddress)
	assert.Equal(t, "127.0.0.1:9090", cfg.AppGRPCAddress)
}

func TestServerConfigSaveAndLoad(t *testing.T) {
	home := t.TempDir()
	configPath := DefaultConfigPath(home)

	cfg := DefaultServerConfig()
	require.NoError(t, cfg.Save(configPath))
	assert.FileExists(t, configPath)

	cfg.ServerListenAddress = "changed"
	require.NoError(t, cfg.Load(configPath))
	assert.Equal(t, "0.0.0.0:7980", cfg.ServerListenAddress)
}

func TestServerConfigSaveIncludesFieldComments(t *testing.T) {
	home := t.TempDir()
	configPath := DefaultConfigPath(home)

	cfg := DefaultServerConfig()
	require.NoError(t, cfg.Save(configPath))

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "# ServerListenAddress is the TCP address where the server listens for requests.")
	assert.Contains(t, content, "server_listen_address =")
	assert.Contains(t, content, "# AppGRPCAddress is the gRPC address of the core/app node.")
	assert.Contains(t, content, "app_grpc_address =")
}

func TestServerConfigLoadCustomFile(t *testing.T) {
	home := t.TempDir()
	configPath := DefaultConfigPath(home)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))

	content := `server_listen_address = "127.0.0.1:8123"
app_grpc_address = "127.0.0.1:10090"
signer_grpc_address = "127.0.0.1:26658"
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	cfg := DefaultServerConfig()
	require.NoError(t, cfg.Load(configPath))
	cfg.Path = home

	assert.Equal(t, "127.0.0.1:8123", cfg.ServerListenAddress)
	assert.Equal(t, "127.0.0.1:10090", cfg.AppGRPCAddress)

	// StoreFn, SignerFn, and StateClientFn are nil until Validate fills in defaults.
	require.NoError(t, cfg.Validate())
	assert.NotNil(t, cfg.StoreFn)
	assert.NotNil(t, cfg.SignerFn)
	assert.NotNil(t, cfg.StateClientFn)
}

func TestServerConfigValidateGRPCSigner(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Path = t.TempDir()
	cfg.SignerGRPCAddress = "127.0.0.1:26660"

	err := cfg.Validate()
	require.NoError(t, err)
	assert.NotNil(t, cfg.SignerFn)
}

func TestServerConfigValidateNoSigner(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Path = t.TempDir()
	cfg.SignerGRPCAddress = ""

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signer_grpc_address is required")
}

func TestServerConfigShardRetention(t *testing.T) {
	home := t.TempDir()
	configPath := DefaultConfigPath(home)

	// the default is written in human-readable form ("4h"), not integer nanoseconds.
	cfg := DefaultServerConfig()
	require.NoError(t, cfg.Save(configPath))
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "shard_retention")
	assert.Contains(t, content, "4h")
	assert.NotContains(t, content, "14400000000000") // not nanoseconds

	// Load reads the value back from the file.
	loaded := DefaultServerConfig()
	loaded.ShardRetention = "ignored"
	require.NoError(t, loaded.Load(configPath))
	assert.Equal(t, "4h", loaded.ShardRetention)

	// Validate parses the string into the cached duration.
	custom := DefaultServerConfig()
	custom.Path = t.TempDir()
	custom.ShardRetention = "2h"
	require.NoError(t, custom.Validate())
	assert.Equal(t, 2*time.Hour, custom.shardRetention)
}

func TestServerConfigValidateShardRetention(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{"unparseable", "banana", "invalid shard_retention"},
		{"non-positive", "0s", "must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultServerConfig()
			cfg.Path = t.TempDir()
			cfg.ShardRetention = tt.value
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
