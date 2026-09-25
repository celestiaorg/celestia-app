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
	assert.Equal(t, 256<<10, cfg.MinUploadSize, "old config files retain the default minimum")

	// StoreFn and StateClientFn are nil until Validate fills in defaults.
	// SignerFn stays nil: the default signer is built at start from the final config.
	require.NoError(t, cfg.Validate())
	assert.NotNil(t, cfg.StoreFn)
	assert.Nil(t, cfg.SignerFn)
	assert.NotNil(t, cfg.StateClientFn)
}

func TestServerConfigValidateGRPCSigner(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Path = t.TempDir()
	cfg.SignerGRPCAddress = "127.0.0.1:26660"

	err := cfg.Validate()
	require.NoError(t, err)
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
		addr             string
		ca, cert, key    string
		allowInsecure    bool
		wantErrSubstring string
	}{
		{name: "all empty"},
		{name: "all set", ca: "ca.pem", cert: "cert.pem", key: "key.pem"},
		{name: "CA only", ca: "ca.pem", wantErrSubstring: "must be set together"},
		{name: "cert without key", ca: "ca.pem", cert: "cert.pem", wantErrSubstring: "must be set together"},
		{name: "cert and key without CA", cert: "cert.pem", key: "key.pem", wantErrSubstring: "must be set together"},
		{name: "plaintext to IPv6 loopback", addr: "[::1]:26669"},
		{name: "plaintext to remote", addr: "10.0.0.5:26669", wantErrSubstring: "is not localhost"},
		{name: "plaintext to hostname", addr: "signer.example.com:26669", wantErrSubstring: "is not localhost"},
		{name: "plaintext to localhost name", addr: "localhost:26669", wantErrSubstring: `use "127.0.0.1" instead of "localhost"`},
		{name: "mTLS to remote", addr: "10.0.0.5:26669", ca: "ca.pem", cert: "cert.pem", key: "key.pem"},
		{name: "plaintext to remote with override", addr: "10.0.0.5:26669", allowInsecure: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultServerConfig()
			cfg.Path = t.TempDir()
			if tc.addr != "" {
				cfg.SignerGRPCAddress = tc.addr
			}
			cfg.SignerGRPCAllowInsecure = tc.allowInsecure
			cfg.SignerGRPCCAFile = tc.ca
			cfg.SignerGRPCCertFile = tc.cert
			cfg.SignerGRPCKeyFile = tc.key

			err := cfg.Validate()
			if tc.wantErrSubstring == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstring)
			}
		})
	}
}

func TestServerConfigSignerTLSPathsResolveAgainstHome(t *testing.T) {
	home := t.TempDir()

	cfg := DefaultServerConfig()
	cfg.Path = home
	cfg.SignerGRPCCAFile = "ca.pem"
	cfg.SignerGRPCCertFile = "cert.pem"
	cfg.SignerGRPCKeyFile = "key.pem"
	require.NoError(t, cfg.Validate())

	// The signer loads the TLS files when constructed: the error names the
	// missing CA resolved against the home directory, not the working directory.
	_, err := cfg.newSigner("test-chain")
	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Join(home, "ca.pem"))

	// Absolute paths are used as-is.
	absCA := filepath.Join(t.TempDir(), "other-ca.pem")
	cfg = DefaultServerConfig()
	cfg.Path = home
	cfg.SignerGRPCCAFile = absCA
	cfg.SignerGRPCCertFile = "cert.pem"
	cfg.SignerGRPCKeyFile = "key.pem"
	require.NoError(t, cfg.Validate())

	_, err = cfg.newSigner("test-chain")
	require.Error(t, err)
	assert.Contains(t, err.Error(), absCA)
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

// TestServerConfigSignerChangedAfterValidate checks that changing the signer
// fields after a successful Validate can't slip a plaintext remote signer
// past the transport checks.
func TestServerConfigSignerChangedAfterValidate(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Path = t.TempDir()
	require.NoError(t, cfg.Validate())

	cfg.SignerGRPCAddress = "10.0.0.5:26669"

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not localhost")

	_, err = cfg.newSigner("test-chain")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not localhost")
}

func TestServerConfigMinUploadSize(t *testing.T) {
	cfg := DefaultServerConfig()
	require.Equal(t, 256<<10, cfg.MinUploadSize)
	path := DefaultConfigPath(t.TempDir())
	cfg.MinUploadSize = 32 << 20
	require.NoError(t, cfg.Save(path))
	loaded := DefaultServerConfig()
	require.NoError(t, loaded.Load(path))
	require.Equal(t, cfg.MinUploadSize, loaded.MinUploadSize)

	for _, size := range []int{-1, 0, 1, 256 << 10, 32 << 20, 128 << 20, (128 << 20) + 1} {
		cfg := DefaultServerConfig()
		cfg.Path = t.TempDir()
		cfg.MinUploadSize = size
		err := cfg.Validate()
		if size < 1 || size > 128<<20 {
			require.ErrorContains(t, err, "min_upload_size")
		} else {
			require.NoError(t, err)
		}
	}
}
