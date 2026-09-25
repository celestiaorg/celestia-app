package fibre

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"

	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/fibre/internal/sign"
	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	toml "github.com/pelletier/go-toml/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const DefaultConfigFileName = "server_config.toml"

// DefaultConfigPath returns the default config file path for the given home directory.
func DefaultConfigPath(home string) string {
	return filepath.Join(home, "config", DefaultConfigFileName)
}

// ServerConfig contains configuration options for the Fibre [Server].
type ServerConfig struct {
	// AppGRPCAddress is the gRPC address of the core/app node.
	AppGRPCAddress string `toml:"app_grpc_address" comment:"AppGRPCAddress is the gRPC address of the core/app node."`
	// ServerListenAddress is the TCP address where the server listens for requests.
	ServerListenAddress string `toml:"server_listen_address" comment:"ServerListenAddress is the TCP address where the server listens for requests."`
	// SignerGRPCAddress is the gRPC address of the validator's PrivValidatorAPI endpoint.
	SignerGRPCAddress string `toml:"signer_grpc_address" comment:"SignerGRPCAddress is the gRPC address of the validator's PrivValidatorAPI endpoint."`
	// SignerGRPCCAFile is the PEM CA certificate used to verify the validator node's
	// server certificate. Set all three TLS files for mTLS; leave all empty for plaintext (localhost only).
	SignerGRPCCAFile string `toml:"signer_grpc_ca_file" comment:"SignerGRPCCAFile is the PEM CA certificate used to verify the validator node's server certificate. Set all three TLS files for mTLS; leave all empty for plaintext (localhost only)."`
	// SignerGRPCCertFile is the PEM client certificate presented to the validator node.
	SignerGRPCCertFile string `toml:"signer_grpc_cert_file" comment:"SignerGRPCCertFile is the PEM client certificate presented to the validator node."`
	// SignerGRPCKeyFile is the PEM private key for the client certificate.
	SignerGRPCKeyFile string `toml:"signer_grpc_key_file" comment:"SignerGRPCKeyFile is the PEM private key for the client certificate."`
	// SignerGRPCAllowInsecure allows a plaintext signer connection to a non-localhost address.
	// DANGER: only use on a network that already restricts access to the signer endpoint.
	SignerGRPCAllowInsecure bool `toml:"signer_grpc_allow_insecure" comment:"SignerGRPCAllowInsecure allows a plaintext signer connection to a non-localhost address. DANGER: only use on a network that already restricts access to the signer endpoint."`
	// UploadVerifyWorkers caps concurrent shard verifications. Defaults to GOMAXPROCS.
	UploadVerifyWorkers int `toml:"upload_verify_workers" comment:"UploadVerifyWorkers caps concurrent shard verifications. Defaults to GOMAXPROCS."`
	// MaxConnections caps total concurrent gRPC connections.
	MaxConnections int `toml:"max_connections" comment:"Max concurrent gRPC connections (default 16). Raise above 16 to keep slots free for downloads during uploads; higher values raise RAM use. See the README for sizing."`
	// MaxConcurrentStreams caps concurrent gRPC streams per connection.
	MaxConcurrentStreams int `toml:"max_concurrent_streams" comment:"Max concurrent gRPC streams per connection (default 13). With max_connections it bounds worst-case RAM (~product x 132 MiB)."`

	StoreConfig `toml:"-"`

	// LivenessThreshold is the fraction of stake needed for reconstruction (typically 1/3).
	LivenessThreshold cmtmath.Fraction `toml:"-"`
	// MinRowsPerValidator is the minimum number of rows each validator must receive
	// for unique decodability security.
	MinRowsPerValidator int `toml:"-"`
	// OriginalRows
	OriginalRows int `toml:"-"`
	// MaxShardSize is the maximum on-disk size of a single shard, used by the
	// storage limiter's startup provisioning checks.
	MaxShardSize int `toml:"-"`
	// MaxMessageSize is the maximum gRPC message size for upload requests.
	MaxMessageSize int `toml:"-"`

	// StoreFn creates the persistent [Store] for the server.
	// If nil, defaults to [NewStore].
	StoreFn func(StoreConfig) (*Store, error) `toml:"-"`
	// StateClientFn creates a [StateClient] for communicating with a celestia-app node.
	// It is called during server construction.
	StateClientFn func() (state.Client, error) `toml:"-"`
	// SignerFn creates a [core.PrivValidator] for the given chain ID.
	// It is called during [Server.Start] after the chain ID is auto-detected.
	// If nil, the server dials the privval gRPC signer configured by the
	// Signer* fields.
	// If the returned value implements io.Closer, it will be closed during [Server.Stop].
	SignerFn func(chainID string) (core.PrivValidator, error) `toml:"-"`

	// UnlimitedBudget disables the storage limiter: an emergency off switch. When
	// false, the server derives its per-node budget from the
	// FullStakeStorageBudget governance parameter via the state client.
	UnlimitedBudget bool `toml:"unlimited_budget"`
	// Log is the logger for the server.
	// If nil, slog.Default() will be used.
	Log *slog.Logger `toml:"-"`
	// Tracer is the OpenTelemetry tracer for distributed tracing.
	// If nil, otel.Tracer("fibre-server") will be used.
	Tracer trace.Tracer `toml:"-"`
	// Meter is the OpenTelemetry meter for recording metrics.
	// If nil, otel.Meter("fibre-server") will be used.
	Meter metric.Meter `toml:"-"`
}

// DefaultServerConfig returns a [ServerConfig] with default values.
func DefaultServerConfig() ServerConfig {
	return NewServerConfigFromParams(DefaultProtocolParams)
}

// NewServerConfigFromParams creates a ServerConfig with values derived from the given ProtocolParams.
// Use this when you need a config with non-default protocol parameters (e.g., for testing).
func NewServerConfigFromParams(p ProtocolParams) ServerConfig {
	cfg := ServerConfig{
		AppGRPCAddress:       "127.0.0.1:9090",
		ServerListenAddress:  "0.0.0.0:7980",
		SignerGRPCAddress:    "127.0.0.1:26669",
		StoreConfig:          DefaultStoreConfig(),
		LivenessThreshold:    p.LivenessThreshold,
		MinRowsPerValidator:  p.MinRowsPerValidator(),
		OriginalRows:         p.Rows,
		MaxShardSize:         p.MaxShardSize(),
		MaxMessageSize:       p.MaxMessageSize(),
		UploadVerifyWorkers:  runtime.GOMAXPROCS(0),
		MaxConnections:       fibregrpc.DefaultMaxConnections,
		MaxConcurrentStreams: fibregrpc.DefaultMaxConcurrentStreams,
	}
	return cfg
}

// Validate validates the ServerConfig and sets default values for unset fields.
func (cfg *ServerConfig) Validate() error {
	if cfg.ServerListenAddress == "" {
		return fmt.Errorf("server listen address is required")
	}

	if cfg.Log == nil {
		cfg.Log = slog.Default().WithGroup("fibre-server")
	}
	if cfg.StoreConfig.Log == nil {
		cfg.StoreConfig.Log = cfg.Log
	}
	if cfg.Tracer == nil {
		cfg.Tracer = otel.Tracer("fibre-server")
	}
	if cfg.Meter == nil {
		cfg.Meter = otel.Meter("fibre-server")
	}

	if cfg.StoreFn == nil {
		if err := cfg.StoreConfig.Validate(); err != nil {
			return fmt.Errorf("store config: %w", err)
		}
		cfg.StoreFn = NewStore
	}

	if cfg.StateClientFn == nil {
		if cfg.AppGRPCAddress == "" {
			return fmt.Errorf("app gRPC address is required for default state client")
		}
		cfg.StateClientFn = func() (state.Client, error) {
			return fibregrpc.NewAppClient(cfg.AppGRPCAddress, cfg.Log)
		}
	}

	// The default signer is built in [ServerConfig.newSigner] from the final
	// field values rather than cached here, so later changes to the config
	// can't bypass these checks.
	if cfg.SignerFn == nil {
		tlsCfg, err := cfg.signerTLSConfig()
		if err != nil {
			return err
		}
		if tlsCfg.Empty() && !dialsLocalhost(cfg.SignerGRPCAddress) {
			cfg.Log.Warn("signer gRPC uses plaintext to a non-localhost address", "address", cfg.SignerGRPCAddress)
		}
	}

	if cfg.UploadVerifyWorkers < 1 {
		return fmt.Errorf("upload_verify_workers must be at least 1, got %d", cfg.UploadVerifyWorkers)
	}
	if cfg.MaxConnections < 1 {
		return fmt.Errorf("max_connections must be at least 1, got %d", cfg.MaxConnections)
	}
	if cfg.MaxConcurrentStreams < 1 {
		return fmt.Errorf("max_concurrent_streams must be at least 1, got %d", cfg.MaxConcurrentStreams)
	}
	if uint64(cfg.MaxConcurrentStreams) > math.MaxUint32 {
		return fmt.Errorf("max_concurrent_streams must not exceed %d, got %d", uint64(math.MaxUint32), cfg.MaxConcurrentStreams)
	}
	return nil
}

// rootify resolves a relative file path against the given root directory.
// Absolute and empty paths are returned unchanged.
func rootify(path, root string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

// dialsLocalhost reports whether the TCP address points at a loopback interface.
func dialsLocalhost(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// signerTLSConfig checks the signer address and TLS policy and returns the
// TLS config to dial the signer with. An empty config means plaintext.
func (cfg *ServerConfig) signerTLSConfig() (*sign.TLSConfig, error) {
	if cfg.SignerGRPCAddress == "" {
		return nil, fmt.Errorf("signer_grpc_address is required")
	}
	tlsSet := cfg.SignerGRPCCAFile != "" || cfg.SignerGRPCCertFile != "" || cfg.SignerGRPCKeyFile != ""
	tlsComplete := cfg.SignerGRPCCAFile != "" && cfg.SignerGRPCCertFile != "" && cfg.SignerGRPCKeyFile != ""
	if tlsSet && !tlsComplete {
		return nil, fmt.Errorf("signer_grpc_ca_file, signer_grpc_cert_file and signer_grpc_key_file must be set together")
	}
	if !tlsSet && !dialsLocalhost(cfg.SignerGRPCAddress) && !cfg.SignerGRPCAllowInsecure {
		return nil, fmt.Errorf("signer_grpc_address %q is not localhost: set signer_grpc_ca_file, signer_grpc_cert_file and signer_grpc_key_file to use mutual TLS, or set signer_grpc_allow_insecure to force plaintext", cfg.SignerGRPCAddress)
	}
	return &sign.TLSConfig{
		CAFile:   rootify(cfg.SignerGRPCCAFile, cfg.Path),
		CertFile: rootify(cfg.SignerGRPCCertFile, cfg.Path),
		KeyFile:  rootify(cfg.SignerGRPCKeyFile, cfg.Path),
	}, nil
}

// newSigner returns the signer for chainID. It uses [ServerConfig.SignerFn]
// if set, otherwise dials the privval gRPC signer, re-checking the transport
// policy against the current field values.
func (cfg *ServerConfig) newSigner(chainID string) (core.PrivValidator, error) {
	if cfg.SignerFn != nil {
		return cfg.SignerFn(chainID)
	}
	tlsCfg, err := cfg.signerTLSConfig()
	if err != nil {
		return nil, err
	}
	return sign.NewGRPCClient(cfg.SignerGRPCAddress, chainID, tlsCfg, cfg.Log)
}

// Load reads the TOML config file at path into the receiver, overriding only
// the TOML-visible fields (those without `toml:"-"`).
// If the file does not exist, Load is a no-op and returns no error.
// Use [ServerConfig.Save] to create the file beforehand if needed.
func (cfg *ServerConfig) Load(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read config file %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("decode config file %s: %w", path, err)
	}
	return nil
}

// Save writes the TOML-visible fields of the config to the given path,
// creating parent directories as needed.
func (cfg ServerConfig) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config dir for %s: %w", path, err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	content := append([]byte("# fibre server configuration\n"), data...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write config file %s: %w", path, err)
	}
	return nil
}
