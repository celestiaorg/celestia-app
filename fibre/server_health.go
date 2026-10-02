package fibre

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	valtypes "github.com/celestiaorg/celestia-app/v10/x/valaddr/types"
	"github.com/cometbft/cometbft/crypto"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// HealthServiceLiveness is the gRPC health service name that is SERVING
// whenever the gRPC server answers. The empty name and the Fibre service name
// report readiness: initialization finished and every check passed.
const HealthServiceLiveness = "fibre-liveness"

var readinessServices = []string{"", "celestia.fibre.v1.Fibre"}

// Reason codes of failed checks, logged on every change.
const (
	reasonAppUnreachable    = "app_unreachable"
	reasonAppSyncing        = "app_syncing"
	reasonChainStalled      = "chain_stalled"
	reasonChainMismatch     = "chain_id_mismatch"
	reasonModuleUnavailable = "fibre_module_unavailable"
	reasonSignerUnreachable = "signer_unreachable"
	reasonSignerKeyChanged  = "signer_key_changed"
	reasonValidatorInactive = "validator_not_active"
	reasonNotRegistered     = "provider_not_registered"
	reasonHostInvalid       = "provider_host_invalid"
	reasonStoreFailed       = "store_failed"
)

// checkResult is the outcome of one check. An empty reason means it passed.
type checkResult struct {
	reason  string
	message string // what to do about it
	err     error  // logged, never served
}

func passed() checkResult { return checkResult{} }

func failed(reason, message string, err error) checkResult {
	return checkResult{reason: reason, message: message, err: err}
}

// healthDeps are the dependencies the checks probe.
type healthDeps struct {
	client state.Client
	signer core.PrivValidator
	pubKey crypto.PubKey // the signer's key at startup
	store  *Store
}

type check struct {
	name string
	run  func(context.Context) checkResult
}

// healthManager runs the checks and publishes readiness to the gRPC health service.
type healthManager struct {
	settings healthSettings
	log      *slog.Logger
	hs       *health.Server

	mu      sync.Mutex
	running bool // between start and stop
	chainID string
	deps    healthDeps
	results map[string]checkResult
	ready   bool
}

func newHealthManager(settings healthSettings, log *slog.Logger) *healthManager {
	m := &healthManager{settings: settings, log: log, hs: health.NewServer(), results: map[string]checkResult{}}
	// health.NewServer marks the empty service SERVING; nothing is ready yet.
	m.set(false, HealthServiceLiveness)
	m.set(false, readinessServices...)
	return m
}

func (m *healthManager) set(serving bool, services ...string) {
	st := healthpb.HealthCheckResponse_NOT_SERVING
	if serving {
		st = healthpb.HealthCheckResponse_SERVING
	}
	for _, s := range services {
		m.hs.SetServingStatus(s, st)
	}
}

func (m *healthManager) registerGRPC(reg grpclib.ServiceRegistrar) {
	healthpb.RegisterHealthServer(reg, healthServer{m.hs})
}

// start marks initialization complete and publishes liveness.
func (m *healthManager) start(deps healthDeps, chainID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running, m.chainID, m.deps = true, chainID, deps
	m.set(true, HealthServiceLiveness)
}

// stop publishes not-ready. Later results are ignored.
func (m *healthManager) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	m.hs.Shutdown() // NOT_SERVING for every service; later status updates are ignored
}

// run executes the checks every check interval until ctx ends, the first time immediately.
func (m *healthManager) run(ctx context.Context) {
	for {
		m.cycle(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.settings.checkInterval):
		}
	}
}

func (m *healthManager) checks() []check {
	return []check{
		{"app", m.checkApp},
		{"fibre_module", m.checkModule},
		{"signer", m.checkSigner},
		{"validator", m.checkValidator},
		{"registration", m.checkRegistration},
		{"store", m.checkStore},
	}
}

// cycle runs the checks one after another, each bounded by the probe timeout.
func (m *healthManager) cycle(ctx context.Context) {
	for _, c := range m.checks() {
		cctx, cancel := context.WithTimeout(ctx, m.settings.probeTimeout)
		m.record(c.name, c.run(cctx))
		cancel()
	}
}

// record stores a result, logs changes and republishes readiness.
func (m *healthManager) record(name string, res checkResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	prev := m.results[name]
	m.results[name] = res
	if prev.reason != res.reason {
		if res.reason == "" {
			m.log.Info("health check passed", "check", name)
		} else {
			m.log.Warn("health check failed", "check", name, "reason", res.reason, "message", res.message, "error", res.err)
		}
	}

	ready, failing := m.evaluate()
	if ready == m.ready {
		return
	}
	m.ready = ready
	m.set(ready, readinessServices...)
	if ready {
		m.log.Info("server ready")
	} else {
		m.log.Warn("server not ready", "failed_checks", failing)
	}
}

// evaluate returns whether the server is ready and which checks fail. The caller holds mu.
func (m *healthManager) evaluate() (bool, []string) {
	var failing []string
	for _, c := range m.checks() {
		if res, found := m.results[c.name]; !found || res.reason != "" {
			failing = append(failing, c.name)
		}
	}
	return m.running && len(failing) == 0, failing
}

func (m *healthManager) checkApp(ctx context.Context) checkResult {
	st, err := m.deps.client.NodeStatus(ctx)
	if err != nil || st.ChainID == "" || st.Height == 0 || st.BlockTime.IsZero() {
		return failed(reasonAppUnreachable, "The app node did not answer a valid status request. Check app_grpc_address and that the node is running.", err)
	}
	if st.ChainID != m.chainID {
		return failed(reasonChainMismatch, fmt.Sprintf("The app node reports chain ID %q, expected %q.", st.ChainID, m.chainID), nil)
	}
	if st.CatchingUp {
		return failed(reasonAppSyncing, "The app node is still syncing.", nil)
	}
	if age := time.Since(st.BlockTime); age > m.settings.maxBlockAge {
		return failed(reasonChainStalled, fmt.Sprintf("The latest block is %s old (limit %s).", age.Truncate(time.Second), m.settings.maxBlockAge), nil)
	}
	return passed()
}

func (m *healthManager) checkModule(ctx context.Context) checkResult {
	_, err := m.deps.client.FullStakeStorageBudget(ctx)
	switch {
	case err == nil:
		return passed()
	case status.Code(err) == codes.Unimplemented:
		return failed(reasonModuleUnavailable, "The app node has no Fibre module. Check that app_grpc_address is the application gRPC endpoint of a chain with Fibre enabled.", err)
	default:
		return failed(reasonAppUnreachable, "The app node did not answer the Fibre module query.", err)
	}
}

// checkSigner proves the signer is reachable and still holds the startup key.
// It does not prove that signing works.
func (m *healthManager) checkSigner(context.Context) checkResult {
	pk, err := m.deps.signer.GetPubKey()
	if err != nil || pk == nil {
		return failed(reasonSignerUnreachable, "The signer did not return a public key. Check signer_grpc_address and the node's priv_validator_grpc_laddr.", err)
	}
	if !bytes.Equal(pk.Bytes(), m.deps.pubKey.Bytes()) {
		return failed(reasonSignerKeyChanged, "The signer reports a different public key than at startup. Confirm the validator key and restart.", nil)
	}
	return passed()
}

func (m *healthManager) checkValidator(ctx context.Context) checkResult {
	set, err := m.deps.client.Head(ctx)
	if err != nil || set.ValidatorSet == nil {
		return failed(reasonAppUnreachable, "Could not fetch the validator set from the app node.", err)
	}
	if val, found := set.GetByAddress(m.deps.pubKey.Address()); !found || val.VotingPower <= 0 {
		return failed(reasonValidatorInactive, fmt.Sprintf("Validator %s is not in the active set.", m.consAddress()), nil)
	}
	return passed()
}

func (m *healthManager) checkRegistration(ctx context.Context) checkResult {
	reg, err := m.deps.client.ProviderRegistration(ctx, m.deps.pubKey.Address())
	switch {
	case err != nil:
		return failed(reasonAppUnreachable, "Could not query the Fibre provider registration from the app node.", err)
	case !reg.Found:
		return failed(reasonNotRegistered, fmt.Sprintf("Register a Fibre provider host for validator %s with MsgSetFibreProviderInfo.", m.consAddress()), nil)
	case valtypes.ValidateHost(reg.Host) != nil:
		return failed(reasonHostInvalid, fmt.Sprintf("The registered provider host %q is not a valid host:port. Register it again.", reg.Host), nil)
	}
	return passed()
}

func (m *healthManager) checkStore(context.Context) checkResult {
	value := binary.BigEndian.AppendUint64(nil, uint64(time.Now().UnixNano()))
	if err := m.deps.store.Probe(value); err != nil {
		return failed(reasonStoreFailed, "The store failed a small write and read. Check free disk space and permissions of the store directory.", err)
	}
	return passed()
}

func (m *healthManager) consAddress() string {
	return sdk.ConsAddress(m.deps.pubKey.Address()).String()
}

// healthServer serves Check from the standard health server and rejects Watch.
// Open Watch streams would hold GracefulStop until the shutdown deadline.
type healthServer struct{ *health.Server }

func (healthServer) Watch(*healthpb.HealthCheckRequest, healthpb.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch is not supported, use Check")
}
