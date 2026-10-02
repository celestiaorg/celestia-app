package fibre

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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

// gRPC health service names. Liveness means the gRPC server answers. Readiness
// means initialization finished and every check passed.
const (
	HealthServiceLiveness  = "fibre-liveness"
	HealthServiceReadiness = "fibre-readiness"
)

// readinessServices are the gRPC health service names that report readiness.
var readinessServices = []string{HealthServiceReadiness, "celestia.fibre.v1.Fibre", ""}

// Check names.
const (
	checkApp          = "app"
	checkModule       = "fibre_module"
	checkSigner       = "signer"
	checkValidator    = "validator"
	checkRegistration = "registration"
	checkStore        = "store"
)

// Server phases.
const (
	phaseStarting = "starting"
	phaseRunning  = "running"
	phaseStopping = "stopping"
)

// Reason codes of failed checks. Automation should key on these.
const (
	reasonNotChecked        = "not_checked"
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

// HealthReport is the JSON body served by GET /readyz.
type HealthReport struct {
	Status       string                 `json:"status"` // ready | not_ready
	Phase        string                 `json:"phase"`  // starting | running | stopping
	ChainID      string                 `json:"chain_id,omitempty"`
	FailedChecks []string               `json:"failed_checks,omitempty"`
	Checks       map[string]HealthCheck `json:"checks"`
}

// HealthCheck is the last result of one check.
type HealthCheck struct {
	Status    string     `json:"status"` // ok | failed | unknown
	Reason    string     `json:"reason,omitempty"`
	Message   string     `json:"message,omitempty"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`

	err error // logged, never served
}

func ok() HealthCheck { return HealthCheck{Status: "ok"} }

func fail(reason, message string, err error) HealthCheck {
	return HealthCheck{Status: "failed", Reason: reason, Message: message, err: err}
}

// healthDeps are the dependencies the checks probe.
type healthDeps struct {
	client state.Client // must not cache
	signer core.PrivValidator
	pubKey crypto.PubKey // the signer's key at startup
	store  *Store
}

type check struct {
	name string
	run  func(context.Context) HealthCheck
}

// healthManager runs the checks and publishes the result to the gRPC health
// service and the HTTP handler. Health requests only read the last results.
type healthManager struct {
	settings healthSettings
	log      *slog.Logger
	hs       *health.Server

	mu      sync.Mutex
	phase   string
	chainID string
	deps    healthDeps
	results map[string]HealthCheck
	ready   bool
}

func newHealthManager(settings healthSettings, log *slog.Logger) *healthManager {
	m := &healthManager{settings: settings, log: log, hs: health.NewServer(), phase: phaseStarting, results: map[string]HealthCheck{}}
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
	m.phase, m.chainID, m.deps = phaseRunning, chainID, deps
	m.set(true, HealthServiceLiveness)
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
		{checkApp, m.checkApp},
		{checkModule, m.checkModule},
		{checkSigner, m.checkSigner},
		{checkValidator, m.checkValidator},
		{checkRegistration, m.checkRegistration},
		{checkStore, m.checkStore},
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

// record stores a result, logs status changes and republishes readiness.
// Results are dropped once the server is stopping.
func (m *healthManager) record(name string, res HealthCheck) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.phase != phaseRunning {
		return
	}
	now := time.Now()
	res.CheckedAt = &now
	prev := m.results[name]
	m.results[name] = res
	if prev.Status != res.Status || prev.Reason != res.Reason {
		if res.Status == "ok" {
			m.log.Info("health check passed", "check", name)
		} else {
			m.log.Warn("health check failed", "check", name, "reason", res.Reason, "message", res.Message, "error", res.err)
		}
	}
	m.publish()
}

// evaluate returns whether the server is ready and which checks fail. The caller holds mu.
func (m *healthManager) evaluate() (bool, []string) {
	if m.phase != phaseRunning {
		return false, nil
	}
	var failing []string
	for _, c := range m.checks() {
		if m.results[c.name].Status != "ok" {
			failing = append(failing, c.name)
		}
	}
	return len(failing) == 0, failing
}

// publish pushes readiness changes to the gRPC health service. The caller holds mu.
func (m *healthManager) publish() {
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

// report returns the current readiness snapshot.
func (m *healthManager) report() HealthReport {
	m.mu.Lock()
	defer m.mu.Unlock()
	ready, failing := m.evaluate()
	rep := HealthReport{Status: "not_ready", Phase: m.phase, ChainID: m.chainID, FailedChecks: failing, Checks: map[string]HealthCheck{}}
	if ready {
		rep.Status = "ready"
	}
	for _, c := range m.checks() {
		res, found := m.results[c.name]
		if !found {
			res = HealthCheck{Status: "unknown", Reason: reasonNotChecked}
		}
		rep.Checks[c.name] = res
	}
	return rep
}

// stop publishes not-ready. Later results are ignored.
func (m *healthManager) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phase = phaseStopping
	m.hs.Shutdown() // NOT_SERVING for every service; later status updates are ignored
}

// httpHandler serves GET /livez and GET /readyz.
func (m *healthManager) httpHandler() http.Handler {
	serve := func(w http.ResponseWriter, ok bool, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(body)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		phase := m.report().Phase
		serve(w, phase != phaseStopping, map[string]string{"phase": phase})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		rep := m.report()
		serve(w, rep.Status == "ready", rep)
	})
	return mux
}

func (m *healthManager) checkApp(ctx context.Context) HealthCheck {
	st, err := m.deps.client.NodeStatus(ctx)
	if err != nil || st.ChainID == "" || st.Height == 0 || st.BlockTime.IsZero() {
		return fail(reasonAppUnreachable, "The app node did not answer a valid status request. Check app_grpc_address and that the node is running.", err)
	}
	if st.ChainID != m.chainID {
		return fail(reasonChainMismatch, fmt.Sprintf("The app node reports chain ID %q, expected %q.", st.ChainID, m.chainID), nil)
	}
	if st.CatchingUp {
		return fail(reasonAppSyncing, "The app node is still syncing.", nil)
	}
	if age := time.Since(st.BlockTime); age > m.settings.maxBlockAge {
		return fail(reasonChainStalled, fmt.Sprintf("The latest block is %s old (limit %s).", age.Truncate(time.Second), m.settings.maxBlockAge), nil)
	}
	return ok()
}

func (m *healthManager) checkModule(ctx context.Context) HealthCheck {
	_, err := m.deps.client.FullStakeStorageBudget(ctx)
	switch {
	case err == nil:
		return ok()
	case status.Code(err) == codes.Unimplemented:
		return fail(reasonModuleUnavailable, "The app node has no Fibre module. Check that app_grpc_address is the application gRPC endpoint of a chain with Fibre enabled.", err)
	default:
		return fail(reasonAppUnreachable, "The app node did not answer the Fibre module query.", err)
	}
}

// checkSigner proves the signer is reachable and still holds the startup key.
// It does not prove that signing works.
func (m *healthManager) checkSigner(context.Context) HealthCheck {
	pk, err := m.deps.signer.GetPubKey()
	if err != nil || pk == nil {
		return fail(reasonSignerUnreachable, "The signer did not return a public key. Check signer_grpc_address and the node's priv_validator_grpc_laddr.", err)
	}
	if !bytes.Equal(pk.Bytes(), m.deps.pubKey.Bytes()) {
		return fail(reasonSignerKeyChanged, "The signer reports a different public key than at startup. Confirm the validator key and restart.", nil)
	}
	return ok()
}

func (m *healthManager) checkValidator(ctx context.Context) HealthCheck {
	set, err := m.deps.client.Head(ctx)
	if err != nil || set.ValidatorSet == nil {
		return fail(reasonAppUnreachable, "Could not fetch the validator set from the app node.", err)
	}
	if val, found := set.GetByAddress(m.deps.pubKey.Address()); !found || val.VotingPower <= 0 {
		return fail(reasonValidatorInactive, fmt.Sprintf("Validator %s is not in the active set.", m.consAddress()), nil)
	}
	return ok()
}

func (m *healthManager) checkRegistration(ctx context.Context) HealthCheck {
	reg, err := m.deps.client.ProviderRegistration(ctx, m.deps.pubKey.Address())
	switch {
	case err != nil:
		return fail(reasonAppUnreachable, "Could not query the Fibre provider registration from the app node.", err)
	case !reg.Found:
		return fail(reasonNotRegistered, fmt.Sprintf("Register a Fibre provider host for validator %s with MsgSetFibreProviderInfo.", m.consAddress()), nil)
	case valtypes.ValidateHost(reg.Host) != nil:
		return fail(reasonHostInvalid, fmt.Sprintf("The registered provider host %q is not a valid host:port. Register it again.", reg.Host), nil)
	}
	return ok()
}

func (m *healthManager) checkStore(context.Context) HealthCheck {
	value := binary.BigEndian.AppendUint64(nil, uint64(time.Now().UnixNano()))
	if err := m.deps.store.Probe(value); err != nil {
		return fail(reasonStoreFailed, "The store failed a small write and read. Check free disk space and permissions of the store directory.", err)
	}
	return ok()
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
