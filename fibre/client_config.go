package fibre

import (
	"fmt"
	"log/slog"
	"time"

	fibregrpc "github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v9/fibre/state"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	clock "github.com/filecoin-project/go-clock"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Upload-path defaults. Sized to match Fibre's 2/3-quorum contract: a
// dead peer must not degrade throughput beyond a one-time cost when its
// circuit first opens.
const (
	// DefaultDialTimeout bounds the initial TCP/TLS dial. Sized well below
	// the ~75 s TCP SYN retry window so a black-holed validator is shed
	// quickly rather than parking a goroutine on a kernel retry loop.
	DefaultDialTimeout = 3 * time.Second
	// DefaultRPCTimeout bounds the full UploadShard RPC after the dial
	// succeeds. Generous enough for slow-but-healthy peers; tight enough
	// that a frozen peer is cut loose.
	DefaultRPCTimeout = 15 * time.Second
	// DefaultCircuitFailureThreshold is the number of consecutive RPC
	// failures to a single peer before its circuit opens and further
	// attempts are short-circuited for DefaultCircuitCooldown.
	DefaultCircuitFailureThreshold = 3
	// DefaultCircuitCooldown is how long a peer's circuit stays open
	// before a half-open probe is allowed.
	DefaultCircuitCooldown = 30 * time.Second
)

// ClientConfig contains configuration options for the Fibre [Client].
type ClientConfig struct {
	// DefaultKeyName is the name of the key in the keyring to use for signing [PaymentPromise]s.
	DefaultKeyName string
	// StateAddress is the gRPC address of the celestia-app node.
	// Used to build the default [StateClientFn] when it is nil.
	StateAddress string

	// SafetyThreshold is the fraction of stake needed to cause a safety failure (typically 2/3).
	SafetyThreshold cmtmath.Fraction
	// LivenessThreshold is the fraction of stake needed to cause a liveness failure (typically 1/3).
	LivenessThreshold cmtmath.Fraction
	// MinRowsPerValidator is the minimum number of rows each validator must receive
	// for unique decodability security.
	MinRowsPerValidator int
	// MaxMessageSize is the maximum gRPC message size for upload requests.
	MaxMessageSize int

	// DialTimeout bounds initial connection establishment to a validator.
	// Sized well below the TCP SYN retry window so a black-holed peer is
	// shed quickly.
	DialTimeout time.Duration

	// RPCTimeout bounds a single UploadShard RPC (after dial succeeds).
	RPCTimeout time.Duration

	// CircuitFailureThreshold is the number of consecutive RPC failures
	// to a single peer before its circuit opens.
	CircuitFailureThreshold int

	// CircuitCooldown is how long a peer's circuit stays open after
	// failures cross the threshold. After cooldown a single half-open
	// probe is allowed; success closes the circuit, failure re-opens it.
	CircuitCooldown time.Duration

	// DownloadConcurrency is the maximum number of concurrent read requests to validators.
	DownloadConcurrency int

	// StateClientFn creates a [state.Client] for communicating with a celestia-app node.
	// If nil, [Validate] creates one from [StateAddress].
	StateClientFn func() (state.Client, error)
	// NewClientFn is the constructor function for creating gRPC [fibregrpc.Client]s.
	// If nil, [Validate] will set a default using the [state.Client]'s [validator.HostRegistry].
	NewClientFn fibregrpc.NewClientFn
	// Log is the logger for the client.
	// If nil, [slog.Default] will be used.
	Log *slog.Logger
	// Tracer is the OpenTelemetry tracer for distributed tracing.
	// If nil, [trace.Default] will be used.
	Tracer trace.Tracer
	// Meter is the OpenTelemetry meter for recording metrics.
	// If nil, otel.Meter("fibre-client") will be used.
	Meter metric.Meter
	// Clock is the clock for time-related operations.
	// If nil, [clock.New] will be used.
	Clock clock.Clock
}

// DefaultClientConfig returns a [ClientConfig] with the default values.
// Values are derived from DefaultProtocolParams.
func DefaultClientConfig() ClientConfig {
	return NewClientConfigFromParams(DefaultProtocolParams)
}

// NewClientConfigFromParams creates a ClientConfig with values derived from the given ProtocolParams.
// Use this when you need a config with non-default protocol parameters (e.g., for testing).
func NewClientConfigFromParams(p ProtocolParams) ClientConfig {
	return ClientConfig{
		DefaultKeyName:          DefaultKeyName,
		StateAddress:            "127.0.0.1:9090",
		SafetyThreshold:         p.SafetyThreshold,
		LivenessThreshold:       p.LivenessThreshold,
		MinRowsPerValidator:     p.MinRowsPerValidator(),
		MaxMessageSize:          p.MaxMessageSize(),
		DialTimeout:             DefaultDialTimeout,
		RPCTimeout:              DefaultRPCTimeout,
		CircuitFailureThreshold: DefaultCircuitFailureThreshold,
		CircuitCooldown:         DefaultCircuitCooldown,
		DownloadConcurrency:     p.ValidatorsForReconstruction(),
	}
}

// Validate validates the ClientConfig and sets default values for unset fields.
func (cfg *ClientConfig) Validate() error {
	if cfg.StateClientFn == nil {
		if cfg.StateAddress == "" {
			return fmt.Errorf("state address is required for default state client")
		}
		cfg.StateClientFn = func() (state.Client, error) {
			return fibregrpc.NewAppClient(cfg.StateAddress, cfg.Log)
		}
	}

	if cfg.Log == nil {
		cfg.Log = slog.Default().WithGroup("fibre-client")
	}
	if cfg.Tracer == nil {
		cfg.Tracer = otel.Tracer("fibre-client")
	}
	if cfg.Meter == nil {
		cfg.Meter = otel.Meter("fibre-client")
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.New()
	}

	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = DefaultDialTimeout
	}
	if cfg.RPCTimeout <= 0 {
		cfg.RPCTimeout = DefaultRPCTimeout
	}
	if cfg.CircuitFailureThreshold <= 0 {
		cfg.CircuitFailureThreshold = DefaultCircuitFailureThreshold
	}
	if cfg.CircuitCooldown <= 0 {
		cfg.CircuitCooldown = DefaultCircuitCooldown
	}
	// A DialTimeout that is >= RPCTimeout is nonsensical: the dial
	// budget would consume the entire RPC budget, leaving no time
	// for the actual UploadShard after connection establishment.
	// Reject explicitly rather than silently producing unusable
	// behavior, since the two knobs are easy to flip by accident.
	if cfg.DialTimeout >= cfg.RPCTimeout {
		return fmt.Errorf("DialTimeout (%s) must be less than RPCTimeout (%s)", cfg.DialTimeout, cfg.RPCTimeout)
	}
	return nil
}
