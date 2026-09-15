package fibre

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
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

func TestServerConfigConnectionDefaults(t *testing.T) {
	cfg := DefaultServerConfig()
	assert.Equal(t, fibregrpc.DefaultMaxConnections, cfg.MaxConnections)
	assert.Equal(t, fibregrpc.DefaultMaxConcurrentStreams, cfg.MaxConcurrentStreams)
}

func TestServerConfigValidateConnectionCaps(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Path = t.TempDir()
	cfg.MaxConnections = 0

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_connections must be at least 1")

	cfg.MaxConnections = fibregrpc.DefaultMaxConnections
	cfg.MaxConcurrentStreams = 0
	err = cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_concurrent_streams must be at least 1")

	tooBig := uint64(math.MaxUint32) + 1
	cfg.MaxConcurrentStreams = int(tooBig)
	err = cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not exceed")
}

func TestServerConfigValidateNoSigner(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Path = t.TempDir()
	cfg.SignerGRPCAddress = ""

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signer_grpc_address is required")
}

func TestServerConfigValidateSignerTLS(t *testing.T) {
	testCases := []struct {
		name             string
		ca, cert, key    string
		wantErrSubstring string
	}{
		{name: "all empty"},
		{name: "all set", ca: "ca.pem", cert: "cert.pem", key: "key.pem"},
		{name: "CA only", ca: "ca.pem", wantErrSubstring: "must be set together"},
		{name: "cert without key", ca: "ca.pem", cert: "cert.pem", wantErrSubstring: "must be set together"},
		{name: "cert and key without CA", cert: "cert.pem", key: "key.pem", wantErrSubstring: "must be set together"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultServerConfig()
			cfg.Path = t.TempDir()
			cfg.SignerGRPCCAFile = tc.ca
			cfg.SignerGRPCCertFile = tc.cert
			cfg.SignerGRPCKeyFile = tc.key

			err := cfg.Validate()
			if tc.wantErrSubstring == "" {
				require.NoError(t, err)
				assert.NotNil(t, cfg.SignerFn)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstring)
			}
		})
	}
}

func TestServerConfigSignerTLSRoundTrip(t *testing.T) {
	home := t.TempDir()
	configPath := DefaultConfigPath(home)

	cfg := DefaultServerConfig()
	cfg.SignerGRPCCAFile = "ca.pem"
	cfg.SignerGRPCCertFile = "cert.pem"
	cfg.SignerGRPCKeyFile = "key.pem"
	require.NoError(t, cfg.Save(configPath))

	loaded := DefaultServerConfig()
	require.NoError(t, loaded.Load(configPath))
	assert.Equal(t, "ca.pem", loaded.SignerGRPCCAFile)
	assert.Equal(t, "cert.pem", loaded.SignerGRPCCertFile)
	assert.Equal(t, "key.pem", loaded.SignerGRPCKeyFile)
}
