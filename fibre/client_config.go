package fibre

import (
	"fmt"
	"log/slog"
	"time"

	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
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

	// RPCTimeout bounds a single UploadShard/DownloadShard call to one peer
	// (dial + RPC), and also the initial validator-set lookup against the
	// celestia-app node (state.Head / state.GetByHeight), so a hung app node
	// cannot stall an Upload/Download before any shard is exchanged. Sheds
	// black-holed peers below the kernel's ~75s TCP SYN retry window. See
	// [DefaultClientConfig] for the default value.
	RPCTimeout time.Duration

	// HostRefreshInterval is the minimum time between on-chain host re-queries
	// for a single validator when a request fails. Defaults to the expected
	// block time, since registry state cannot change faster than one block.
	HostRefreshInterval time.Duration

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

	// Escrow configures client-side escrow auto-funding so uploads don't fail
	// when the escrow account runs low.
	Escrow EscrowConfig
}

// defaultEscrowConfig derives escrow auto-funding defaults from the protocol
// params. Watermarks are sized as multiples of the maximum single-blob payment
// so that even a max-size blob is always admissible and a burst of in-flight
// max-size blobs is comfortably covered before a refill lands.
func defaultEscrowConfig(p ProtocolParams) EscrowConfig {
	maxBlobPayment := fibretypes.PaymentAmount(uint32(p.MaxBlobSize)).Amount
	return EscrowConfig{
		// Opt-in: auto-funding broadcasts on-chain MsgDepositToEscrow txs, so it
		// stays off by default until a caller explicitly enables it.
		AutoFund:            false,
		LowWatermark:        maxBlobPayment.MulRaw(2),
		HighWatermark:       maxBlobPayment.MulRaw(10),
		RefillCheckInterval: time.Second,
	}
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
		RPCTimeout:          15 * time.Second,
		HostRefreshInterval: fibregrpc.DefaultRefreshInterval,
		Escrow:              defaultEscrowConfig(p),
	}
}

// Validate validates the ClientConfig and sets default values for unset fields.
func (cfg *ClientConfig) Validate() error {
	if cfg.StateClientFn == nil {
		if cfg.StateAddress == "" {
			return fmt.Errorf("state address is required for default state client")
		}
		cfg.StateClientFn = func() (state.Client, error) {
			return fibregrpc.NewAppClient(cfg.StateAddress, cfg.Log,
				fibregrpc.WithClock(cfg.Clock),
				fibregrpc.WithRefreshInterval(cfg.HostRefreshInterval),
				fibregrpc.WithQueryTimeout(cfg.RPCTimeout),
			)
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
	if cfg.HostRefreshInterval <= 0 {
		cfg.HostRefreshInterval = fibregrpc.DefaultRefreshInterval
	}

	if cfg.RPCTimeout <= 0 {
		return fmt.Errorf("RPCTimeout must be > 0 (see [DefaultClientConfig])")
	}

	if err := cfg.Escrow.Validate(); err != nil {
		return fmt.Errorf("escrow config: %w", err)
	}
	return nil
}

// Validate fills unset fields with defaults and rejects invalid combinations.
// Watermarks and the check interval are sanitized whether or not AutoFund is
// set, since the ledger uses them (RefillCheckInterval) even when funding is
// disabled.
func (e *EscrowConfig) Validate() error {
	d := defaultEscrowConfig(DefaultProtocolParams)
	if e.LowWatermark.IsNil() {
		e.LowWatermark = d.LowWatermark
	}
	if e.HighWatermark.IsNil() {
		e.HighWatermark = d.HighWatermark
	}
	if e.RefillCheckInterval <= 0 {
		e.RefillCheckInterval = d.RefillCheckInterval
	}
	if !e.LowWatermark.IsPositive() {
		return fmt.Errorf("low_watermark must be > 0, got %s", e.LowWatermark)
	}
	if e.HighWatermark.LTE(e.LowWatermark) {
		return fmt.Errorf("high_watermark (%s) must be > low_watermark (%s)", e.HighWatermark, e.LowWatermark)
	}
	return nil
}
