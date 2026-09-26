package fibre

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"

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
	// MinUploadSize is the local minimum padded upload size, excluding parity, in bytes.
	MinUploadSize int `toml:"min_upload_size" comment:"Minimum padded Fibre upload size in bytes, including header and excluding parity (default 262144). Restart Fibre after changing."`
	// UploadVerifyWorkers caps concurrent shard verifications. Defaults to GOMAXPROCS.
	UploadVerifyWorkers int `toml:"upload_verify_workers" comment:"UploadVerifyWorkers caps concurrent shard verifications. Defaults to GOMAXPROCS."`
	// MaxConnections caps total concurrent gRPC connections.
	MaxConnections int `toml:"max_connections" comment:"Max concurrent gRPC connections (default 16). Raise above 16 to keep slots free for downloads during uploads; higher values raise RAM use. See the README for sizing."`
	// MaxConcurrentStreams caps concurrent gRPC streams per connection.
	MaxConcurrentStreams int `toml:"max_concurrent_streams" comment:"Max concurrent gRPC streams per connection (default 13). With max_connections it bounds worst-case RAM (~product x 132 MiB)."`
	// HealthListenAddress optionally serves GET /livez and GET /readyz over HTTP. Empty disables it;
	// the gRPC health service on ServerListenAddress is always on.
	HealthListenAddress string `toml:"health_listen_address" comment:"Optional HTTP address serving GET /livez and GET /readyz, e.g. 127.0.0.1:7981. Empty disables it; gRPC health on the server listen address is always on."`
	// Health configures the readiness checks.
	Health HealthConfig `toml:"health" comment:"Dependency checks behind the gRPC health service and the HTTP health endpoints."`

	StoreConfig

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
	StoreFn func(context.Context, StoreConfig) (*Store, error) `toml:"-"`
	// StateClientFn creates a [StateClient] for communicating with a celestia-app node.
	// It is called during server construction.
	StateClientFn func() (state.Client, error) `toml:"-"`
	// SignerFn creates a [core.PrivValidator] for the given chain ID.
	// It is called during [Server.Start] after the chain ID is auto-detected.
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

	health healthSettings // parsed Health, populated by Validate
}

// HealthConfig configures the readiness checks. Durations use Go syntax such as "10s".
// A successful check result stays valid for two check intervals plus one probe timeout.
type HealthConfig struct {
	ExpectedChainID string `toml:"expected_chain_id" comment:"Chain ID the app node must report. Empty auto-detects it, which cannot notice a wrong network at first startup."`
	CheckInterval   string `toml:"check_interval" comment:"How often the app node, Fibre module, signer, validator membership, provider registration and store are checked."`
	ProbeTimeout    string `toml:"probe_timeout" comment:"Deadline of each check. A successful result stays valid for two check intervals plus one timeout."`
	MaxBlockAge     string `toml:"max_block_age" comment:"Maximum age of the app node's latest block before the chain is reported as stalled. Tune it to the network's block time."`
}

type healthSettings struct {
	expectedChainID                                        string
	checkInterval, probeTimeout, maxResultAge, maxBlockAge time.Duration
}

// DefaultHealthConfig returns the default [HealthConfig].
func DefaultHealthConfig() HealthConfig {
	return HealthConfig{CheckInterval: "10s", ProbeTimeout: "3s", MaxBlockAge: "2m"}
}

func (cfg HealthConfig) parse() (healthSettings, error) {
	s := healthSettings{expectedChainID: cfg.ExpectedChainID}
	for _, f := range []struct {
		name, value string
		dst         *time.Duration
	}{{"check_interval", cfg.CheckInterval, &s.checkInterval}, {"probe_timeout", cfg.ProbeTimeout, &s.probeTimeout}, {"max_block_age", cfg.MaxBlockAge, &s.maxBlockAge}} {
		d, err := time.ParseDuration(f.value)
		if err != nil || d <= 0 {
			return s, fmt.Errorf("health.%s must be a positive duration, got %q", f.name, f.value)
		}
		*f.dst = d
	}
	s.maxResultAge = 2*s.checkInterval + s.probeTimeout
	return s, nil
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
		Health:               DefaultHealthConfig(),
		StoreConfig:          DefaultStoreConfig(),
		LivenessThreshold:    p.LivenessThreshold,
		MinRowsPerValidator:  p.MinRowsPerValidator(),
		OriginalRows:         p.Rows,
		MaxShardSize:         p.MaxShardSize(),
		MaxMessageSize:       p.MaxMessageSize(),
		MinUploadSize:        p.Rows * p.MinRowSize,
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
	health, err := cfg.Health.parse()
	if err != nil {
		return fmt.Errorf("health config: %w", err)
	}
	cfg.health = health

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

	if cfg.SignerFn == nil {
		if cfg.SignerGRPCAddress == "" {
			return fmt.Errorf("signer_grpc_address is required")
		}
		cfg.SignerFn = func(chainID string) (core.PrivValidator, error) {
			return sign.NewGRPCClient(cfg.SignerGRPCAddress, chainID, cfg.Log)
		}
	}

	if cfg.MinUploadSize < 1 || cfg.MinUploadSize > DefaultProtocolParams.MaxBlobSize {
		return fmt.Errorf("min_upload_size must be between 1 and %d bytes, got %d", DefaultProtocolParams.MaxBlobSize, cfg.MinUploadSize)
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
