package fibre

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	fibregrpc "github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v9/fibre/internal/sign"
	"github.com/celestiaorg/celestia-app/v9/fibre/state"
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

	StoreConfig `toml:"-"`

	// LivenessThreshold is the fraction of stake needed for reconstruction (typically 1/3).
	LivenessThreshold cmtmath.Fraction `toml:"-"`
	// MinRowsPerValidator is the minimum number of rows each validator must receive
	// for unique decodability security.
	MinRowsPerValidator int `toml:"-"`
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
	// If the returned value implements io.Closer, it will be closed during [Server.Stop].
	SignerFn func(chainID string) (core.PrivValidator, error) `toml:"-"`

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
		AppGRPCAddress:      "127.0.0.1:9090",
		ServerListenAddress: "0.0.0.0:7980",
		SignerGRPCAddress:   "127.0.0.1:26659",
		StoreConfig:         DefaultStoreConfig(),
		LivenessThreshold:   p.LivenessThreshold,
		MinRowsPerValidator: p.MinRowsPerValidator(),
		MaxMessageSize:      p.MaxMessageSize(),
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

	if cfg.SignerFn == nil {
		if cfg.SignerGRPCAddress == "" {
			return fmt.Errorf("signer_grpc_address is required")
		}
		cfg.SignerFn = func(chainID string) (core.PrivValidator, error) {
			return sign.NewGRPCClient(cfg.SignerGRPCAddress, chainID, cfg.Log)
		}
	}
	return nil
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
