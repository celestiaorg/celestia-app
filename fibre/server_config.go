package fibre

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	fibregrpc "github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v9/fibre/internal/sign"
	"github.com/celestiaorg/celestia-app/v9/fibre/state"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	clock "github.com/filecoin-project/go-clock"
	toml "github.com/pelletier/go-toml/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const DefaultConfigFileName = "server_config.toml"

// DefaultUploadRateLimitBytesPerSecond is the default server upload admission
// rate. "10 MB/s" is interpreted as 10 MiB/s. It is charged as
// [PaymentPromise.UploadSize] (the whole-blob upload size) per UploadShard, so
// each validator independently approximates the network's global PFF
// blob-admission throughput. The controller is disabled when
// [ServerConfig.UploadRateLimitEnabled] is false OR this rate is <= 0.
const DefaultUploadRateLimitBytesPerSecond = 10 * 1024 * 1024

// uploadProcessingMargin is the headroom added to the worst-case server-side
// rate-limit wait (burst/rate, i.e. MaxBlobSize/rate) when deriving the
// client's upload timeout. It covers verify + store + sign + dial + network so
// a client can tolerate a validator that waits out the rate limiter while still
// reaching quorum. Kept far below the ~75s kernel TCP SYN window that
// black-hole peer shedding relies on.
const uploadProcessingMargin = 12 * time.Second

// maxUploadRateLimitWait returns the worst-case time a request waits for the
// upload rate limiter to admit a maximum-size blob from a drained bucket at the
// default rate: burst/rate, where burst is the largest admissible upload
// (MaxRowSize * Rows, == MaxBlobSize for default params). It is the single
// source for both the server's default UploadRateLimitMaxWait and the client's
// UploadTimeout, so the two stay in sync.
func maxUploadRateLimitWait(p ProtocolParams) time.Duration {
	burst := p.MaxRowSize(0) * p.Rows
	return time.Duration(burst) * time.Second / time.Duration(DefaultUploadRateLimitBytesPerSecond)
}

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
	// UploadVerifyWorkers caps concurrent shard verifications. Defaults to GOMAXPROCS.
	UploadVerifyWorkers int `toml:"upload_verify_workers" comment:"UploadVerifyWorkers caps concurrent shard verifications. Defaults to GOMAXPROCS."`

	// UploadRateLimitEnabled toggles the upload admission controller (both the
	// byte-throughput limit and the in-flight cap). The controller is active only
	// when this is true AND UploadRateLimitBytesPerSecond > 0.
	UploadRateLimitEnabled bool `toml:"upload_rate_limit_enabled" comment:"UploadRateLimitEnabled toggles the upload admission controller (byte-throughput limit and in-flight cap). Active only when true and upload_rate_limit_bytes_per_second > 0."`
	// UploadRateLimitBytesPerSecond caps admitted upload throughput in bytes/sec,
	// charged as PaymentPromise.UploadSize per UploadShard. A value <= 0 disables
	// the entire upload admission controller (rate limit AND in-flight cap).
	UploadRateLimitBytesPerSecond int `toml:"upload_rate_limit_bytes_per_second" comment:"UploadRateLimitBytesPerSecond caps admitted upload throughput in bytes/sec, charged as the whole-blob UploadSize per UploadShard. <= 0 disables the upload admission controller (rate limit and in-flight cap)."`
	// UploadRateLimitBurstBytes is the token-bucket burst in bytes. Defaults to the
	// maximum admissible upload size so any single blob fits without debt; it also
	// bounds the largest instantaneous burst admitted from a full bucket.
	UploadRateLimitBurstBytes int `toml:"upload_rate_limit_burst_bytes" comment:"UploadRateLimitBurstBytes is the token-bucket burst in bytes. Defaults to the maximum blob size so any single blob fits without debt."`
	// UploadRateLimitMaxWait caps how long a request waits for byte budget before
	// returning ResourceExhausted, as a Go duration string (e.g. "12.8s").
	// Defaults to burst/rate so a single max-size blob is admitted after the
	// necessary wait; stacked contention beyond this is rejected. The wait is also
	// bounded by the request context.
	UploadRateLimitMaxWait string `toml:"upload_rate_limit_max_wait" comment:"UploadRateLimitMaxWait caps how long a request waits for byte budget before ResourceExhausted, as a Go duration string (e.g. \"12.8s\"). Defaults to burst/rate (also bounded by the request context)."`
	// MaxUploadShardInFlight caps the number of UploadShard handlers admitted past
	// verification at once. A full cap returns ResourceExhausted immediately.
	MaxUploadShardInFlight int `toml:"max_upload_shard_in_flight" comment:"MaxUploadShardInFlight caps concurrent UploadShard handlers admitted past verification. A full cap returns ResourceExhausted immediately."`

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
	// Clock is the clock used by the upload admission controller for rate
	// accounting and bounded waits. If nil, [clock.New] will be used. It uses
	// server observation time, never the client-controlled promise timestamp.
	Clock clock.Clock `toml:"-"`
}

// DefaultServerConfig returns a [ServerConfig] with default values.
func DefaultServerConfig() ServerConfig {
	return NewServerConfigFromParams(DefaultProtocolParams)
}

// NewServerConfigFromParams creates a ServerConfig with values derived from the given ProtocolParams.
// Use this when you need a config with non-default protocol parameters (e.g., for testing).
func NewServerConfigFromParams(p ProtocolParams) ServerConfig {
	cfg := ServerConfig{
		AppGRPCAddress:                "127.0.0.1:9090",
		ServerListenAddress:           "0.0.0.0:7980",
		SignerGRPCAddress:             "127.0.0.1:26659",
		StoreConfig:                   DefaultStoreConfig(),
		LivenessThreshold:             p.LivenessThreshold,
		MinRowsPerValidator:           p.MinRowsPerValidator(),
		MaxMessageSize:                p.MaxMessageSize(),
		UploadVerifyWorkers:           runtime.GOMAXPROCS(0),
		UploadRateLimitEnabled:        true,
		UploadRateLimitBytesPerSecond: DefaultUploadRateLimitBytesPerSecond,
		// The largest UploadSize verifyShard accepts is MaxRowSize * Rows (rowSize
		// is capped at MaxRowSize, an upload has at most OriginalRows == p.Rows
		// rows), == MaxBlobSize (128 MiB) for default params. A burst >= this
		// guarantees a single blob never exceeds the token bucket.
		UploadRateLimitBurstBytes: p.MaxRowSize(0) * p.Rows,
		UploadRateLimitMaxWait:    maxUploadRateLimitWait(p).String(),
		MaxUploadShardInFlight:    max(32, 2*runtime.GOMAXPROCS(0)),
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
	if cfg.Clock == nil {
		cfg.Clock = clock.New()
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

	if cfg.UploadVerifyWorkers < 1 {
		return fmt.Errorf("upload_verify_workers must be at least 1, got %d", cfg.UploadVerifyWorkers)
	}

	// Upload admission controller. The other knobs are only used when the
	// controller is active.
	if cfg.uploadRateLimitActive() {
		if cfg.UploadRateLimitBurstBytes <= 0 {
			return fmt.Errorf("upload_rate_limit_burst_bytes must be > 0 when rate limiting is enabled, got %d", cfg.UploadRateLimitBurstBytes)
		}
		wait, err := cfg.uploadRateLimitMaxWait()
		if err != nil {
			return fmt.Errorf("upload_rate_limit_max_wait: %w", err)
		}
		if wait < 0 {
			return fmt.Errorf("upload_rate_limit_max_wait must be >= 0, got %s", wait)
		}
		if cfg.MaxUploadShardInFlight < 1 {
			return fmt.Errorf("max_upload_shard_in_flight must be >= 1 when rate limiting is enabled, got %d", cfg.MaxUploadShardInFlight)
		}
	}
	return nil
}

// uploadRateLimitMaxWait parses [ServerConfig.UploadRateLimitMaxWait] into a
// duration. An empty string means no wait (0).
func (cfg *ServerConfig) uploadRateLimitMaxWait() (time.Duration, error) {
	if cfg.UploadRateLimitMaxWait == "" {
		return 0, nil
	}
	return time.ParseDuration(cfg.UploadRateLimitMaxWait)
}

// uploadRateLimitActive reports whether the upload admission controller is
// active: enabled by the toggle AND configured with a positive rate.
func (cfg *ServerConfig) uploadRateLimitActive() bool {
	return cfg.UploadRateLimitEnabled && cfg.UploadRateLimitBytesPerSecond > 0
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
