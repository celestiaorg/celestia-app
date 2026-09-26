package fibre

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// fakeDeps is a state.Client and state.HealthClient whose answers tests mutate under mu.
type fakeDeps struct {
	mu        sync.Mutex
	status    state.NodeStatus
	statusErr error
	set       validator.Set
	reg       state.ProviderRegistration
	moduleErr error
	priv      crypto.PrivKey
	signerErr error
	block     chan struct{} // GetPubKey waits on it when set
}

func (f *fakeDeps) NodeStatus(context.Context) (state.NodeStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.statusErr
}

func (f *fakeDeps) ValidatorSetAt(context.Context, uint64) (validator.Set, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.set, nil
}

func (f *fakeDeps) ProviderRegistration(context.Context, core.Address) (state.ProviderRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reg, nil
}

func (f *fakeDeps) GetPubKey() (crypto.PubKey, error) {
	f.mu.Lock()
	block, priv, err := f.block, f.priv, f.signerErr
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	return priv.PubKey(), err
}

func (f *fakeDeps) SignRawBytes(chainID, uniqueID string, raw []byte) ([]byte, error) {
	b, err := core.RawBytesMessageSignBytes(chainID, uniqueID, raw)
	if err != nil {
		return nil, err
	}
	return f.priv.Sign(b)
}
func (f *fakeDeps) SignVote(string, *cmtproto.Vote) error                      { return nil }
func (f *fakeDeps) SignProposal(string, *cmtproto.Proposal) error              { return nil }
func (f *fakeDeps) Head(context.Context) (validator.Set, error)                { return f.set, nil }
func (f *fakeDeps) GetByHeight(context.Context, uint64) (validator.Set, error) { return f.set, nil }
func (f *fakeDeps) GetHost(context.Context, *core.Validator) (validator.Host, error) {
	return "", errors.New("unused")
}
func (f *fakeDeps) ChainID() string                                       { return "test-chain" }
func (f *fakeDeps) FullStakeStorageBudget(context.Context) (int64, error) { return 0, f.moduleErr }
func (f *fakeDeps) Start(context.Context) error                           { return nil }
func (f *fakeDeps) Stop(context.Context) error                            { return nil }
func (f *fakeDeps) VerifyPromise(context.Context, *state.PaymentPromise) (state.VerifiedPromise, error) {
	return state.VerifiedPromise{}, nil
}

func newFakeDeps() *fakeDeps {
	priv := ed25519.GenPrivKey()
	return &fakeDeps{
		status: state.NodeStatus{ChainID: "test-chain", Height: 42, BlockTime: time.Now()},
		set:    validator.Set{ValidatorSet: core.NewValidatorSet([]*core.Validator{core.NewValidator(priv.PubKey(), 10)}), Height: 42},
		reg:    state.ProviderRegistration{Found: true, Host: "127.0.0.1:7980"},
		priv:   priv,
	}
}

func TestHealthManager(t *testing.T) {
	deps := newFakeDeps()
	store := NewMemoryStore(StoreConfig{})
	t.Cleanup(func() { _ = store.Close() })
	settings := healthSettings{checkInterval: 20 * time.Millisecond, probeTimeout: 100 * time.Millisecond, maxResultAge: 140 * time.Millisecond, maxBlockAge: time.Minute}
	m := newHealthManager(settings, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.phase, m.chainID = phaseRunning, "test-chain"
	m.serving.Store(true)
	m.deps = healthDeps{client: deps, module: func(context.Context) error { return deps.moduleErr }, signer: deps, store: store}
	ctx := context.Background()
	readiness := func() healthpb.HealthCheckResponse_ServingStatus {
		resp, err := m.hs.Check(ctx, &healthpb.HealthCheckRequest{Service: HealthServiceReadiness})
		require.NoError(t, err)
		return resp.GetStatus()
	}

	require.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, readiness())
	m.cycle(ctx)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, readiness())
	rep := m.report()
	require.Equal(t, "ready", rep.Status)
	assert.NotEmpty(t, rep.Checks[checkSigner].ConsensusAddress)
	assert.Equal(t, "127.0.0.1:7980", rep.Checks[checkRegistration].RegisteredHost)
	assert.Equal(t, uint64(42), rep.Checks[checkValidator].Height)

	mutations := []struct {
		check, reason string
		mutate        func()
	}{
		{checkApp, reasonAppUnreachable, func() { deps.statusErr = errors.New("down") }},
		{checkApp, reasonChainMismatch, func() { deps.status.ChainID = "other" }},
		{checkApp, reasonAppSyncing, func() { deps.status.CatchingUp = true }},
		{checkApp, reasonChainStalled, func() { deps.status.BlockTime = time.Now().Add(-time.Hour) }},
		{checkModule, reasonModuleUnavailable, func() { deps.moduleErr = status.Error(codes.Unimplemented, "unknown service") }},
		{checkModule, reasonAppUnreachable, func() { deps.moduleErr = status.Error(codes.DeadlineExceeded, "timeout") }},
		{checkSigner, reasonSignerUnreachable, func() { deps.signerErr = errors.New("down") }},
		{checkValidator, reasonValidatorInactive, func() { deps.set = validator.Set{ValidatorSet: core.NewValidatorSet(nil), Height: 42} }},
		{checkRegistration, reasonNotRegistered, func() { deps.reg.Found = false }},
		{checkRegistration, reasonHostInvalid, func() { deps.reg.Host = "no-port" }},
	}
	healthy := newFakeDeps()
	healthy.priv, healthy.set = deps.priv, deps.set
	restore := func() {
		deps.mu.Lock()
		deps.status, deps.statusErr, deps.set, deps.reg, deps.moduleErr, deps.signerErr, deps.priv = healthy.status, nil, healthy.set, healthy.reg, nil, nil, healthy.priv
		deps.mu.Unlock()
	}
	for _, tc := range mutations {
		deps.mu.Lock()
		tc.mutate()
		deps.mu.Unlock()
		m.cycle(ctx)
		assert.Equal(t, tc.reason, m.report().Checks[tc.check].Reason, tc.check)
		assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, readiness(), tc.reason)
		restore()
		m.cycle(ctx)
		assert.Equal(t, healthpb.HealthCheckResponse_SERVING, readiness(), "recovery after "+tc.reason)
	}

	// Dependent checks are skipped, not run, while the signer fails; a key change is a failure.
	deps.mu.Lock()
	deps.signerErr = errors.New("down")
	deps.mu.Unlock()
	m.cycle(ctx)
	assert.Equal(t, reasonDependency, m.report().Checks[checkValidator].Reason)
	deps.mu.Lock()
	deps.signerErr, deps.priv = nil, ed25519.GenPrivKey()
	deps.mu.Unlock()
	m.cycle(ctx)
	assert.Equal(t, reasonSignerKeyChanged, m.report().Checks[checkSigner].Reason)
	restore()

	// A probe that ignores its context times out, is not restarted while running, and its late result is dropped.
	m.cycle(ctx)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, readiness())
	deps.mu.Lock()
	deps.block = make(chan struct{})
	deps.mu.Unlock()
	m.cycle(ctx)
	assert.Equal(t, reasonTimeout, m.report().Checks[checkSigner].Reason)
	deps.mu.Lock()
	block := deps.block
	deps.block = nil
	deps.mu.Unlock()
	m.cycle(ctx) // the first probe is still blocked: no second probe, result unchanged
	assert.Equal(t, reasonTimeout, m.report().Checks[checkSigner].Reason)
	close(block)
	require.Eventually(t, func() bool { m.cycle(ctx); return readiness() == healthpb.HealthCheckResponse_SERVING }, 5*time.Second, 20*time.Millisecond)

	// Results expire without new probes and reach gRPC health through the timer.
	require.Eventually(t, func() bool { return readiness() == healthpb.HealthCheckResponse_NOT_SERVING }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, reasonStale, m.report().Checks[checkApp].Reason)
	m.cycle(ctx)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, readiness())

	// A state client without probe support never becomes ready and never panics.
	m.deps.client = nil
	m.cycle(ctx)
	assert.Equal(t, reasonUnsupported, m.report().Checks[checkRegistration].Reason)
	m.deps.client = deps
	m.cycle(ctx)

	// Shutdown publishes not-ready and ignores later results.
	require.True(t, m.stop(ctx))
	m.record(checkApp, HealthCheck{Status: "ok"})
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, readiness())
	assert.Equal(t, phaseStopping, m.report().Reason)
}

// newTestServer builds a server on fake dependencies; a non-nil gate holds Start at the store open.
func newTestServer(t *testing.T, deps *fakeDeps, gate chan struct{}) *Server {
	t.Helper()
	cfg := DefaultServerConfig()
	cfg.ServerListenAddress, cfg.HealthListenAddress, cfg.UnlimitedBudget = "127.0.0.1:0", "127.0.0.1:0", true
	cfg.Health.CheckInterval, cfg.Health.ProbeTimeout = "50ms", "1s"
	cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg.StateClientFn = func() (state.Client, error) { return deps, nil }
	cfg.SignerFn = func(string) (core.PrivValidator, error) { return deps, nil }
	cfg.StoreFn = func(ctx context.Context, scfg StoreConfig) (*Store, error) {
		if gate != nil {
			<-gate
		}
		return NewMemoryStore(scfg), nil
	}
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })
	return srv
}

func livez(t *testing.T, srv *Server) int {
	t.Helper()
	resp, err := http.Get("http://" + srv.HealthListenAddress() + "/livez") //nolint:gosec // test URL
	require.NoError(t, err)
	resp.Body.Close()
	return resp.StatusCode
}

// TestServerHealth covers the lifecycle over real listeners: health answers before the store is open
// while Fibre RPCs are gated, readiness follows the checks, HTTP agrees, and Watch streams end at Stop.
func TestServerHealth(t *testing.T) {
	deps, gate := newFakeDeps(), make(chan struct{})
	srv := newTestServer(t, deps, gate)
	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start(context.Background()) }()

	conn, err := grpclib.NewClient(srv.ListenAddress(), grpclib.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // self-signed identity cert, as grpc_health_probe -tls-no-verify
		MinVersion:         tls.VersionTLS13,
	})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	health, ctx := healthpb.NewHealthClient(conn), context.Background()
	check := func(service string) healthpb.HealthCheckResponse_ServingStatus {
		resp, err := health.Check(ctx, &healthpb.HealthCheckRequest{Service: service})
		if err != nil {
			return healthpb.HealthCheckResponse_UNKNOWN
		}
		return resp.GetStatus()
	}
	readyz := func() (int, HealthReport) {
		resp, err := http.Get("http://" + srv.HealthListenAddress() + "/readyz") //nolint:gosec // test URL
		require.NoError(t, err)
		defer resp.Body.Close()
		var rep HealthReport
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&rep))
		return resp.StatusCode, rep
	}

	require.Eventually(t, func() bool { return check(HealthServiceLiveness) == healthpb.HealthCheckResponse_SERVING }, 10*time.Second, 20*time.Millisecond)
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, check(HealthServiceReadiness))
	code, _ := readyz()
	assert.Equal(t, http.StatusServiceUnavailable, code)
	_, err = types.NewFibreClient(conn).DownloadShard(ctx, &types.DownloadShardRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err), "Fibre RPCs are gated until initialization completes")

	close(gate)
	require.NoError(t, <-startErr)
	require.Eventually(t, func() bool { return check("") == healthpb.HealthCheckResponse_SERVING }, 10*time.Second, 20*time.Millisecond)
	_, err = types.NewFibreClient(conn).DownloadShard(ctx, &types.DownloadShardRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "the gate is open")
	_, err = types.NewFibreClient(conn).DownloadShard(ctx, &types.DownloadShardRequest{BlobId: NewBlobID(0, Commitment{1})})
	assert.Equal(t, codes.NotFound, status.Code(err))
	code, rep := readyz()
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, HealthActivity{DownloadMisses: 1}, rep.Activity, "misses for shards this server does not hold are counted, uploads are zero")

	stream, err := health.Watch(ctx, &healthpb.HealthCheckRequest{Service: HealthServiceReadiness})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.NoError(t, err)
	start := time.Now()
	require.NoError(t, srv.Stop(ctx))
	require.Less(t, time.Since(start), 5*time.Second, "an open Watch stream must not hold the drain")
	for err == nil {
		_, err = stream.Recv()
	}
	require.NoError(t, srv.Stop(ctx), "repeated stop")
}

// TestServerStopDuringStart checks that Stop waits for a blocked Start and then releases what Start
// acquired, and that a stopped server cannot start again.
func TestServerStopDuringStart(t *testing.T) {
	gate := make(chan struct{})
	srv := newTestServer(t, newFakeDeps(), gate)
	startErr, stopErr := make(chan error, 1), make(chan error, 1)
	go func() { startErr <- srv.Start(context.Background()) }()
	require.Eventually(t, func() bool { return livez(t, srv) == http.StatusOK }, 10*time.Second, 20*time.Millisecond)
	go func() { stopErr <- srv.Stop(context.Background()) }()
	select {
	case err := <-stopErr:
		t.Fatalf("Stop returned %v while Start was blocked", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(gate)
	require.NoError(t, <-startErr)
	require.NoError(t, <-stopErr)
	assert.True(t, srv.store.closed.Load(), "Stop closes the store Start opened")
	require.Error(t, srv.Start(context.Background()))
}

// TestServerServeFailure checks that an unexpected gRPC exit is reported through Done and fails liveness.
func TestServerServeFailure(t *testing.T) {
	srv := newTestServer(t, newFakeDeps(), nil)
	require.NoError(t, srv.Start(context.Background()))
	require.NoError(t, srv.grpc.Listener().Close())
	select {
	case <-srv.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done was not closed after the listener failed")
	}
	require.Error(t, srv.Err())
	require.Eventually(t, func() bool { return livez(t, srv) == http.StatusServiceUnavailable }, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, srv.Stop(context.Background()))
}
