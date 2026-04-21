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

// DefaultUploadShardTimeout is the default per-RPC deadline for a single
// UploadShard call. Matches 2× the observed p99 for healthy uploads
// (~2.5 s) with headroom — generous for slow-but-healthy validators,
// tight enough that a network-black-holed or struggling validator can't
// hold a goroutine / semaphore slot for the full TCP SYN retry window.
const DefaultUploadShardTimeout = 10 * time.Second

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

	// UploadConcurrency is the maximum number of concurrent uploads to validators.
	UploadConcurrency int
	// DownloadConcurrency is the maximum number of concurrent read requests to validators.
	DownloadConcurrency int

	// UploadShardTimeout bounds a single UploadShard RPC (dial + send +
	// validator signature response). Without this bound, a validator that
	// goes silent (network partition, frozen process, saturated peer) can
	// park its client goroutine and semaphore slot for the full TCP SYN
	// retry window (75+ seconds), which collapses upload throughput across
	// the whole cluster even though the 2/3 quorum threshold is met by
	// healthy validators.
	// Zero → DefaultUploadShardTimeout.
	UploadShardTimeout time.Duration

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
		DefaultKeyName:      DefaultKeyName,
		StateAddress:        "127.0.0.1:9090",
		SafetyThreshold:     p.SafetyThreshold,
		LivenessThreshold:   p.LivenessThreshold,
		MinRowsPerValidator: p.MinRowsPerValidator(),
		MaxMessageSize:      p.MaxMessageSize(),
		UploadConcurrency:   p.MaxValidatorCount,
		DownloadConcurrency: p.ValidatorsForReconstruction(),
		UploadShardTimeout:  DefaultUploadShardTimeout,
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
	if cfg.UploadShardTimeout <= 0 {
		cfg.UploadShardTimeout = DefaultUploadShardTimeout
	}
	return nil
}
