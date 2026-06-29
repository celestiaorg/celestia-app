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

func TestServerConfigRateLimitDefaults(t *testing.T) {
	cfg := DefaultServerConfig()
	assert.True(t, cfg.UploadRateLimitEnabled)
	// "10 MB/s" is interpreted as 10 MiB/s.
	assert.Equal(t, 10*1024*1024, cfg.UploadRateLimitBytesPerSecond)
	// Burst defaults to the max admissible upload size (128 MiB for default params).
	assert.Equal(t, 128*1024*1024, cfg.UploadRateLimitBurstBytes)
	// MaxWait defaults to burst/rate, here 128 MiB / 10 MiB/s == 12.8s.
	wait, err := cfg.uploadRateLimitMaxWait()
	require.NoError(t, err)
	assert.Equal(t, 12800*time.Millisecond, wait)
	assert.GreaterOrEqual(t, cfg.MaxUploadShardInFlight, 32)
}

func TestServerConfigRateLimitSaveAndLoad(t *testing.T) {
	home := t.TempDir()
	configPath := DefaultConfigPath(home)

	cfg := DefaultServerConfig()
	cfg.UploadRateLimitEnabled = true
	cfg.UploadRateLimitBytesPerSecond = 5 * 1024 * 1024
	cfg.UploadRateLimitBurstBytes = 64 * 1024 * 1024
	cfg.UploadRateLimitMaxWait = "7s"
	cfg.MaxUploadShardInFlight = 48
	require.NoError(t, cfg.Save(configPath))

	// Saved TOML carries the new keys and their documentation.
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "upload_rate_limit_enabled =")
	assert.Contains(t, content, "upload_rate_limit_bytes_per_second =")
	assert.Contains(t, content, "upload_rate_limit_burst_bytes =")
	assert.Contains(t, content, "upload_rate_limit_max_wait =")
	assert.Contains(t, content, "max_upload_shard_in_flight =")

	loaded := DefaultServerConfig()
	require.NoError(t, loaded.Load(configPath))
	assert.Equal(t, 5*1024*1024, loaded.UploadRateLimitBytesPerSecond)
	assert.Equal(t, 64*1024*1024, loaded.UploadRateLimitBurstBytes)
	assert.Equal(t, "7s", loaded.UploadRateLimitMaxWait)
	assert.Equal(t, 48, loaded.MaxUploadShardInFlight)
}

func TestServerConfigValidateRateLimit(t *testing.T) {
	baseEnabled := func() ServerConfig {
		cfg := DefaultServerConfig()
		cfg.Path = t.TempDir()
		cfg.SignerGRPCAddress = "127.0.0.1:26659"
		return cfg
	}

	t.Run("disabled rate skips other rate-limit validation", func(t *testing.T) {
		cfg := baseEnabled()
		cfg.UploadRateLimitBytesPerSecond = 0 // disables the controller
		cfg.UploadRateLimitBurstBytes = 0
		cfg.UploadRateLimitMaxWait = ""
		cfg.MaxUploadShardInFlight = 0
		require.NoError(t, cfg.Validate())
	})

	t.Run("enabled with zero burst errors", func(t *testing.T) {
		cfg := baseEnabled()
		cfg.UploadRateLimitBurstBytes = 0
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "upload_rate_limit_burst_bytes")
	})

	t.Run("enabled with unparseable max wait errors", func(t *testing.T) {
		cfg := baseEnabled()
		cfg.UploadRateLimitMaxWait = "not-a-duration"
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "upload_rate_limit_max_wait")
	})

	t.Run("enabled with negative max wait errors", func(t *testing.T) {
		cfg := baseEnabled()
		cfg.UploadRateLimitMaxWait = "-1s"
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "upload_rate_limit_max_wait")
	})

	t.Run("enabled with zero in-flight errors", func(t *testing.T) {
		cfg := baseEnabled()
		cfg.MaxUploadShardInFlight = 0
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "max_upload_shard_in_flight")
	})

	t.Run("defaults validate", func(t *testing.T) {
		cfg := baseEnabled()
		require.NoError(t, cfg.Validate())
	})
}
