package fibre

import (
	"fmt"
	"log/slog"

	fibregrpc "github.com/celestiaorg/celestia-app/v8/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v8/fibre/state"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	clock "github.com/filecoin-project/go-clock"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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

	// UploadConcurrency is the maximum number of concurrent uploads to validators.
	UploadConcurrency int
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
		DefaultKeyName:      DefaultKeyName,
		StateAddress:        "127.0.0.1:9090",
		SafetyThreshold:     p.SafetyThreshold,
		LivenessThreshold:   p.LivenessThreshold,
		MinRowsPerValidator: p.MinRowsPerValidator(),
		MaxMessageSize:      p.MaxMessageSize(),
		UploadConcurrency:   p.MaxValidatorCount,
		DownloadConcurrency: p.ValidatorsForReconstruction(),
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
	return nil
}
