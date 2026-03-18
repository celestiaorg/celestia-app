package fibre

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/cometbft/cometbft/crypto/ed25519"
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
signer_pub_key = "` + hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes()) + `"
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
	validPubKey := hex.EncodeToString(ed25519.GenPrivKey().PubKey().Bytes())

	tests := []struct {
		name      string
		grpcAddr  string
		pubKey    string
		wantErr   string
		expectNil bool // if true, expect no error
	}{
		{
			name:      "valid gRPC signer config",
			grpcAddr:  "127.0.0.1:26660",
			pubKey:    validPubKey,
			expectNil: true,
		},
		{
			name:     "gRPC address without pub key",
			grpcAddr: "127.0.0.1:26660",
			pubKey:   "",
			wantErr:  "signer_pub_key is required",
		},
		{
			name:     "gRPC address with invalid hex pub key",
			grpcAddr: "127.0.0.1:26660",
			pubKey:   "not-valid-hex",
			wantErr:  "invalid signer_pub_key hex",
		},
		{
			name:     "gRPC address with wrong length pub key",
			grpcAddr: "127.0.0.1:26660",
			pubKey:   hex.EncodeToString([]byte("tooshort")),
			wantErr:  "must be 32 bytes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultServerConfig()
			cfg.Path = t.TempDir()
			cfg.SignerGRPCAddress = tc.grpcAddr
			cfg.SignerPubKey = tc.pubKey

			err := cfg.Validate()
			if tc.expectNil {
				require.NoError(t, err)
				assert.NotNil(t, cfg.SignerFn)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

func TestServerConfigValidateNoSigner(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.Path = t.TempDir()
	cfg.SignerGRPCAddress = ""

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signer_grpc_address is required")
}
