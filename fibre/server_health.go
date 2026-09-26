package fibre

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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

// gRPC health service names. Liveness only reflects the gRPC server; readiness
// means initialization finished and every dependency check passed recently.
// The Fibre service name and the empty name alias readiness.
const (
	HealthServiceLiveness  = "fibre-liveness"
	HealthServiceReadiness = "fibre-readiness"
)

// Check names, phases and reason codes exposed by the readiness report. Automation should key on these.
const (
	checkApp, checkModule, checkSigner, checkValidator, checkRegistration, checkStore = "app", "fibre_module", "signer", "validator", "registration", "store"
	phaseStarting, phaseRunning, phaseStopping, phaseFailed                           = "starting", "running", "stopping", "failed"

	reasonChecksFailed, reasonNotChecked, reasonStale, reasonTimeout, reasonUnsupported, reasonDependency                = "checks_failed", "not_checked", "stale", "probe_timeout", "unsupported", "dependency_failed"
	reasonAppUnreachable, reasonAppSyncing, reasonChainStalled, reasonChainMismatch, reasonModuleUnavailable             = "app_unreachable", "app_syncing", "chain_stalled", "chain_id_mismatch", "fibre_module_unavailable"
	reasonSignerUnreachable, reasonSignerKeyChanged, reasonValidatorSetUnavailable, reasonValidatorInactive              = "signer_unreachable", "signer_key_changed", "validator_set_unavailable", "validator_not_active"
	reasonRegistrationUnavailable, reasonNotRegistered, reasonHostInvalid, reasonStoreWriteFailed, reasonStoreReadFailed = "registration_unavailable", "provider_not_registered", "provider_host_invalid", "store_write_failed", "store_read_failed"
)

var (
	checkOrder        = []string{checkApp, checkModule, checkSigner, checkValidator, checkRegistration, checkStore}
	readinessServices = []string{HealthServiceReadiness, "celestia.fibre.v1.Fibre", ""}
)

// HealthReport is the readiness snapshot served by GET /readyz. Reachability from
// outside and end-to-end uploads cannot be verified from inside the process and
// are reported as not_checked.
type HealthReport struct {
	Status               string                 `json:"status"` // ready | not_ready
	Reason               string                 `json:"reason,omitempty"`
	FailedChecks         []string               `json:"failed_checks,omitempty"`
	Phase                string                 `json:"phase"`
	ChainID              string                 `json:"chain_id,omitempty"`
	ChainIDSource        string                 `json:"chain_id_source"` // configured | auto_detected
	CheckedAt            time.Time              `json:"checked_at"`
	Checks               map[string]HealthCheck `json:"checks"`
	Activity             HealthActivity         `json:"activity"`
	ExternalReachability string                 `json:"external_reachability"`
	EndToEndUpload       string                 `json:"end_to_end_upload"`
}

// HealthActivity counts request traffic since startup. It is informational and
// never affects readiness: a ready server with no uploads, or with many misses
// for shards it does not hold, points at registration, reachability or clients.
type HealthActivity struct {
	Uploads        int64      `json:"uploads"`
	UploadFailures int64      `json:"upload_failures"`
	LastUploadAt   *time.Time `json:"last_upload_at,omitempty"`
	Downloads      int64      `json:"downloads"`
	DownloadMisses int64      `json:"download_misses"` // "no blob shard found" responses
	LastDownloadAt *time.Time `json:"last_download_at,omitempty"`
}

// HealthCheck is one dependency check within a [HealthReport].
type HealthCheck struct {
	Status           string     `json:"status"` // unknown | ok | failed
	Reason           string     `json:"reason,omitempty"`
	Message          string     `json:"message,omitempty"`
	CheckedAt        *time.Time `json:"checked_at,omitempty"`
	LastSuccess      *time.Time `json:"last_success,omitempty"`
	Height           uint64     `json:"height,omitempty"`
	ConsensusAddress string     `json:"consensus_address,omitempty"`
	RegisteredHost   string     `json:"registered_host,omitempty"`

	err    error // logged locally, never exposed
	pubKey crypto.PubKey
}

func fail(reason, message string, err error) HealthCheck {
	return HealthCheck{Status: "failed", Reason: reason, Message: message, err: err}
}

// healthDeps are the dependencies probed for readiness; a nil client keeps the server unready.
type healthDeps struct {
	client state.HealthClient
	module func(context.Context) error // any Fibre module query
	signer core.PrivValidator
	store  *Store
}

// healthManager owns the phase, the check results and the readiness decision; health requests
// only read the snapshot and never trigger probes.
type healthManager struct {
	settings healthSettings
	log      *slog.Logger
	hs       *health.Server
	shutdown chan struct{}
	serving  atomic.Bool // gate for Fibre RPCs

	mu       sync.Mutex
	phase    string
	chainID  string
	deps     healthDeps
	results  map[string]HealthCheck
	running  map[string]bool
	identity crypto.PubKey // first confirmed signer key
	ready    bool
	expiry   *time.Timer
	loopStop context.CancelFunc
	loopDone chan struct{}
	probes   sync.WaitGroup

	uploads, uploadFailures, lastUpload, downloads, downloadMisses, lastDownload atomic.Int64
}

// noteUpload and noteDownload record request outcomes for the activity block of
// the report. They accept a nil manager so handlers work on a bare Server.
func (m *healthManager) noteUpload(err error) {
	switch {
	case m == nil:
	case err != nil:
		m.uploadFailures.Add(1)
	default:
		m.uploads.Add(1)
		m.lastUpload.Store(time.Now().UnixNano())
	}
}

func (m *healthManager) noteDownload(err error) {
	switch {
	case m == nil:
	case err == nil:
		m.downloads.Add(1)
		m.lastDownload.Store(time.Now().UnixNano())
	case status.Code(err) == codes.NotFound:
		m.downloadMisses.Add(1)
	}
}

func unixPtr(ns int64) *time.Time {
	if ns == 0 {
		return nil
	}
	t := time.Unix(0, ns)
	return &t
}

func newHealthManager(settings healthSettings, log *slog.Logger) *healthManager {
	m := &healthManager{
		settings: settings, log: log, hs: health.NewServer(), shutdown: make(chan struct{}),
		phase: phaseStarting, results: map[string]HealthCheck{}, running: map[string]bool{},
	}
	m.set(false, HealthServiceLiveness) // health.NewServer marks the empty service SERVING; nothing is ready yet
	m.set(false, readinessServices...)
	return m
}

func (m *healthManager) set(ok bool, services ...string) {
	st := healthpb.HealthCheckResponse_NOT_SERVING
	if ok {
		st = healthpb.HealthCheckResponse_SERVING
	}
	for _, s := range services {
		m.hs.SetServingStatus(s, st)
	}
}

func (m *healthManager) registerGRPC(reg grpclib.ServiceRegistrar) {
	healthpb.RegisterHealthServer(reg, &healthService{Server: m.hs, shutdown: m.shutdown})
}

// start marks initialization complete, opens the RPC gate and runs check cycles, the first one immediately.
func (m *healthManager) start(ctx context.Context, deps healthDeps, chainID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phase, m.chainID, m.deps = phaseRunning, chainID, deps
	m.serving.Store(true)
	ctx, m.loopStop = context.WithCancel(ctx)
	m.loopDone = make(chan struct{})
	go func() {
		defer close(m.loopDone)
		for {
			m.cycle(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(m.settings.checkInterval):
			}
		}
	}()
}

// cycle runs the independent checks concurrently, then the ones that need the signer identity and the app height.
func (m *healthManager) cycle(ctx context.Context) {
	m.runAll(ctx, map[string]func(context.Context) HealthCheck{checkApp: m.checkApp, checkModule: m.checkModule, checkSigner: m.checkSigner, checkStore: m.checkStore})
	m.mu.Lock()
	identity, app := m.identity, m.results[checkApp]
	signerOK := identity != nil && m.results[checkSigner].Status == "ok"
	m.mu.Unlock()
	skipped := func(dep string) func(context.Context) HealthCheck {
		return func(context.Context) HealthCheck {
			return fail(reasonDependency, "Not checked because the "+dep+" check failed.", nil)
		}
	}
	dependent := map[string]func(context.Context) HealthCheck{checkValidator: skipped(checkSigner), checkRegistration: skipped(checkSigner)}
	if signerOK {
		dependent[checkRegistration] = func(ctx context.Context) HealthCheck { return m.checkRegistration(ctx, identity.Address()) }
		dependent[checkValidator] = skipped(checkApp)
		if app.Status == "ok" {
			dependent[checkValidator] = func(ctx context.Context) HealthCheck { return m.checkValidator(ctx, app.Height, identity.Address()) }
		}
	}
	m.runAll(ctx, dependent)
}

func (m *healthManager) runAll(ctx context.Context, checks map[string]func(context.Context) HealthCheck) {
	var wg sync.WaitGroup
	for name, fn := range checks {
		wg.Go(func() { m.run(ctx, name, fn) })
	}
	wg.Wait()
}

// run executes one check bounded by the probe timeout. A check that ignores its context keeps
// running: the timeout is recorded now, its late result is discarded, and it is not started again.
func (m *healthManager) run(ctx context.Context, name string, fn func(context.Context) HealthCheck) {
	m.mu.Lock()
	if m.running[name] {
		m.mu.Unlock()
		return
	}
	m.running[name] = true
	m.mu.Unlock()
	pctx, cancel := context.WithTimeout(ctx, m.settings.probeTimeout)
	defer cancel()
	done := make(chan HealthCheck, 1)
	m.probes.Go(func() {
		res := fn(pctx)
		m.mu.Lock()
		m.running[name] = false
		m.mu.Unlock()
		done <- res
	})
	select {
	case res := <-done:
		m.record(name, res)
	case <-pctx.Done():
		m.record(name, fail(reasonTimeout, fmt.Sprintf("The %s check did not finish within %s; no new check starts until it returns.", name, m.settings.probeTimeout), pctx.Err()))
	}
}

// record stores a result, logs transitions and republishes readiness; results are dropped after shutdown began.
func (m *healthManager) record(name string, res HealthCheck) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.phase != phaseRunning {
		return
	}
	if name == checkSigner && res.Status == "ok" {
		if m.identity == nil {
			m.identity = res.pubKey
		} else if !bytes.Equal(m.identity.Bytes(), res.pubKey.Bytes()) {
			res = fail(reasonSignerKeyChanged, "The signer now reports a different public key than at startup. Confirm the validator key and restart.", nil)
		}
		if res.Status == "ok" {
			res.ConsensusAddress = sdk.ConsAddress(res.pubKey.Address()).String()
		}
	}
	now := time.Now()
	prev := m.results[name]
	res.CheckedAt, res.LastSuccess = &now, prev.LastSuccess
	if res.Status == "ok" {
		res.LastSuccess = &now
	}
	m.results[name] = res
	if prev.Status != res.Status || prev.Reason != res.Reason {
		if attrs := []any{"check", name, "reason", res.Reason, "message", res.Message}; res.Status == "ok" {
			m.log.Info("health check passed", "check", name)
		} else if res.err != nil {
			m.log.Warn("health check failed", append(attrs, "error", res.err)...)
		} else {
			m.log.Warn("health check failed", attrs...)
		}
	}
	m.publish()
}

func (m *healthManager) stale(c HealthCheck, now time.Time) bool {
	return c.CheckedAt == nil || now.Sub(*c.CheckedAt) > m.settings.maxResultAge
}

func (m *healthManager) evaluate(now time.Time) (bool, []string) {
	if m.phase != phaseRunning {
		return false, nil
	}
	var failing []string
	for _, name := range checkOrder {
		if c := m.results[name]; c.Status != "ok" || m.stale(c, now) {
			failing = append(failing, name)
		}
	}
	return len(failing) == 0, failing
}

// publish pushes readiness changes to gRPC health and arms a timer that re-evaluates when results
// exceed their maximum age, so stale results reach Watch subscribers even when no probe completes.
func (m *healthManager) publish() {
	now := time.Now()
	ready, failing := m.evaluate(now)
	if ready != m.ready {
		m.ready = ready
		m.set(ready, readinessServices...)
		if ready {
			m.log.Info("server ready", "chain_id", m.chainID)
		} else {
			m.log.Warn("server not ready", "phase", m.phase, "failed_checks", failing)
		}
	}
	if m.expiry != nil {
		m.expiry.Stop()
		m.expiry = nil
	}
	if ready { // every result is at most one probe timeout old
		m.expiry = time.AfterFunc(m.settings.maxResultAge+time.Millisecond, func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.phase == phaseRunning {
				m.publish()
			}
		})
	}
}

// report builds the readiness snapshot; successful results past their maximum age are reported as failed/stale.
func (m *healthManager) report() HealthReport {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	ready, failing := m.evaluate(now)
	rep := HealthReport{
		Status: "not_ready", Reason: m.phase, Phase: m.phase, ChainID: m.chainID, ChainIDSource: "auto_detected", CheckedAt: now,
		Checks: make(map[string]HealthCheck, len(checkOrder)), ExternalReachability: "not_checked", EndToEndUpload: "not_checked",
	}
	if m.settings.expectedChainID != "" {
		rep.ChainIDSource = "configured"
	}
	rep.Activity = HealthActivity{
		Uploads: m.uploads.Load(), UploadFailures: m.uploadFailures.Load(), LastUploadAt: unixPtr(m.lastUpload.Load()),
		Downloads: m.downloads.Load(), DownloadMisses: m.downloadMisses.Load(), LastDownloadAt: unixPtr(m.lastDownload.Load()),
	}
	if ready {
		rep.Status, rep.Reason = "ready", ""
	} else if m.phase == phaseRunning {
		rep.Reason, rep.FailedChecks = reasonChecksFailed, failing
	}
	for _, name := range checkOrder {
		c, ok := m.results[name]
		if !ok {
			c = HealthCheck{Status: "unknown", Reason: reasonNotChecked}
		} else if c.Status == "ok" && m.stale(c, now) {
			c.Status, c.Reason = "failed", reasonStale
			c.Message = fmt.Sprintf("The last successful %s check is %s old (limit %s); the check may be stalled.", name, now.Sub(*c.CheckedAt).Truncate(time.Second), m.settings.maxResultAge)
		}
		rep.Checks[name] = c
	}
	return rep
}

// stop publishes not-ready, ends Watch streams, stops the loop and waits for
// running probes until ctx expires. It returns false when a probe is still stuck
// in I/O; the caller must then not close the dependency it uses.
func (m *healthManager) stop(ctx context.Context) bool {
	m.mu.Lock()
	if m.phase != phaseStopping {
		m.phase = phaseStopping
		m.serving.Store(false)
		m.publish()
		m.hs.Shutdown() // NOT_SERVING everywhere; later status updates are ignored
		close(m.shutdown)
		m.log.Info("health: server stopping, published not ready")
	}
	loopStop, loopDone := m.loopStop, m.loopDone
	m.mu.Unlock()
	if loopStop != nil {
		loopStop()
		<-loopDone
	}
	drained := make(chan struct{})
	go func() { m.probes.Wait(); close(drained) }()
	select {
	case <-drained:
		return true
	case <-ctx.Done():
		return false
	}
}

// serveFailed records an unexpected gRPC server exit: liveness and readiness both fail.
func (m *healthManager) serveFailed(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.phase == phaseStarting || m.phase == phaseRunning {
		m.log.Error("gRPC server stopped unexpectedly", "error", err)
		m.phase = phaseFailed
		m.serving.Store(false)
		m.set(false, HealthServiceLiveness)
		m.publish()
	}
}

// unaryGate rejects Fibre RPCs until initialization completes and once shutdown began.
func (m *healthManager) unaryGate(ctx context.Context, req any, info *grpclib.UnaryServerInfo, handler grpclib.UnaryHandler) (any, error) {
	if m.serving.Load() || strings.HasPrefix(info.FullMethod, "/grpc.health.v1.Health/") {
		return handler(ctx, req)
	}
	return nil, status.Error(codes.Unavailable, "fibre server is not ready")
}

// httpHandler serves GET /livez and GET /readyz from the snapshot with Cache-Control: no-store.
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
		serve(w, phase == phaseStarting || phase == phaseRunning, map[string]string{"phase": phase})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		rep := m.report()
		serve(w, rep.Status == "ready", rep)
	})
	return mux
}

// unsupported reports a state client without probe support; the server then never becomes ready.
func (m *healthManager) unsupported() (HealthCheck, bool) {
	if m.deps.client != nil {
		return HealthCheck{}, false
	}
	return fail(reasonUnsupported, "The configured state client cannot run health probes; use the default app client.", nil), true
}

func (m *healthManager) checkApp(ctx context.Context) HealthCheck {
	if c, ok := m.unsupported(); ok {
		return c
	}
	st, err := m.deps.client.NodeStatus(ctx)
	age := time.Since(st.BlockTime)
	switch {
	case err != nil, st.ChainID == "" || st.Height == 0 || st.BlockTime.IsZero():
		return fail(reasonAppUnreachable, "The app node did not answer a valid status request. Check app_grpc_address and that the node is running.", err)
	case m.settings.expectedChainID != "" && st.ChainID != m.settings.expectedChainID:
		return fail(reasonChainMismatch, fmt.Sprintf("The app node reports chain ID %q but expected_chain_id is %q.", st.ChainID, m.settings.expectedChainID), nil)
	case st.ChainID != m.chainID:
		return fail(reasonChainMismatch, fmt.Sprintf("The app node now reports chain ID %q but %q was detected at startup; restart if the endpoint changed on purpose.", st.ChainID, m.chainID), nil)
	case st.CatchingUp:
		return fail(reasonAppSyncing, "The app node is still syncing.", nil)
	case age > m.settings.maxBlockAge:
		return fail(reasonChainStalled, fmt.Sprintf("The latest block is %s old (limit %s); the app node may be stalled or disconnected.", age.Truncate(time.Second), m.settings.maxBlockAge), nil)
	}
	return HealthCheck{Status: "ok", Height: st.Height}
}

// checkModule queries the Fibre module. Unimplemented means the node answers but has no Fibre module; anything else means it did not answer.
func (m *healthManager) checkModule(ctx context.Context) HealthCheck {
	err := m.deps.module(ctx)
	switch {
	case err == nil:
		return HealthCheck{Status: "ok"}
	case status.Code(err) == codes.Unimplemented:
		return fail(reasonModuleUnavailable, "The app node does not serve celestia.fibre.v1.Query: the chain has not activated the Fibre module or app_grpc_address is not the application gRPC endpoint.", err)
	default:
		return fail(reasonAppUnreachable, "The app node did not answer the Fibre module query. Check app_grpc_address and that the node is running.", err)
	}
}

// checkSigner performs a real public key RPC. It proves signer access and identity, not that signing works.
func (m *healthManager) checkSigner(ctx context.Context) HealthCheck {
	var (
		pk  crypto.PubKey
		err error
	)
	if s, ok := m.deps.signer.(interface {
		GetPubKeyContext(context.Context) (crypto.PubKey, error)
	}); ok {
		pk, err = s.GetPubKeyContext(ctx)
	} else {
		pk, err = m.deps.signer.GetPubKey()
	}
	if err != nil || pk == nil || len(pk.Bytes()) == 0 {
		return fail(reasonSignerUnreachable, "The signer did not return a valid public key. Check signer_grpc_address and the node's priv_validator_grpc_laddr.", err)
	}
	return HealthCheck{Status: "ok", Message: "Public key RPC succeeded; signing is only verified by real uploads.", pubKey: pk}
}

func (m *healthManager) checkValidator(ctx context.Context, height uint64, addr crypto.Address) HealthCheck {
	if c, ok := m.unsupported(); ok {
		return c
	}
	set, err := m.deps.client.ValidatorSetAt(ctx, height)
	if err != nil || set.ValidatorSet == nil {
		return fail(reasonValidatorSetUnavailable, fmt.Sprintf("Could not fetch the validator set at height %d; retried next cycle.", height), err)
	}
	if val, found := set.GetByAddress(addr); !found || val.VotingPower <= 0 {
		return fail(reasonValidatorInactive, fmt.Sprintf("The signer's validator is not in the active set at height %d with positive voting power.", height), nil)
	}
	return HealthCheck{Status: "ok", Height: height}
}

func (m *healthManager) checkRegistration(ctx context.Context, addr crypto.Address) HealthCheck {
	if c, ok := m.unsupported(); ok {
		return c
	}
	reg, err := m.deps.client.ProviderRegistration(ctx, addr)
	switch {
	case err != nil:
		return fail(reasonRegistrationUnavailable, "Could not query the Fibre provider registration from the app node.", err)
	case !reg.Found:
		return fail(reasonNotRegistered, "Register this validator's Fibre provider host on chain with MsgSetFibreProviderInfo.", nil)
	case valtypes.ValidateHost(reg.Host) != nil:
		return fail(reasonHostInvalid, fmt.Sprintf("The registered provider host %q is not a valid host:port; re-register it.", reg.Host), nil)
	}
	return HealthCheck{Status: "ok", RegisteredHost: reg.Host}
}

func (m *healthManager) checkStore(ctx context.Context) HealthCheck {
	err := m.deps.store.Probe(ctx, binary.BigEndian.AppendUint64(nil, uint64(time.Now().UnixNano())))
	switch {
	case err == nil:
		return HealthCheck{Status: "ok"}
	case errors.Is(err, ErrStoreProbeRead):
		return fail(reasonStoreReadFailed, "The store could not read back the probe value; the database may be corrupted or closed.", err)
	default:
		return fail(reasonStoreWriteFailed, "The store rejected a small synced write. Check free disk space and permissions of the store directory.", err)
	}
}

// healthService ends Watch streams at shutdown so GracefulStop cannot wait on them until the deadline.
type healthService struct {
	*health.Server
	shutdown <-chan struct{}
}

func (h *healthService) Watch(in *healthpb.HealthCheckRequest, stream healthpb.Health_WatchServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	go func() {
		select {
		case <-h.shutdown:
			cancel()
		case <-ctx.Done():
		}
	}()
	err := h.Server.Watch(in, watchStream{stream, ctx})
	if ctx.Err() != nil && stream.Context().Err() == nil {
		_ = stream.Send(&healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_NOT_SERVING})
	}
	return err
}

type watchStream struct {
	healthpb.Health_WatchServer
	ctx context.Context
}

func (w watchStream) Context() context.Context { return w.ctx }
