package fibre

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// fakeDeps is a state.Client and a core.PrivValidator whose answers tests change under mu.
// Methods the health checks do not call are left to the embedded nil interfaces.
type fakeDeps struct {
	state.Client
	core.PrivValidator

	mu        sync.Mutex
	status    state.NodeStatus
	statusErr error
	set       validator.Set
	setErr    error
	reg       state.ProviderRegistration
	moduleErr error
	priv      crypto.PrivKey
	signerErr error

	signerBlock chan struct{} // if set, GetPubKey waits for it
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

func (f *fakeDeps) NodeStatus(context.Context) (state.NodeStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.statusErr
}

func (f *fakeDeps) Head(context.Context) (validator.Set, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.set, f.setErr
}

func (f *fakeDeps) ProviderRegistration(context.Context, core.Address) (state.ProviderRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reg, nil
}

func (f *fakeDeps) FullStakeStorageBudget(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return 0, f.moduleErr
}

func (f *fakeDeps) GetPubKey() (crypto.PubKey, error) {
	if f.signerBlock != nil {
		<-f.signerBlock
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.priv.PubKey(), f.signerErr
}

func (f *fakeDeps) SignRawBytes(chainID, uniqueID string, raw []byte) ([]byte, error) {
	b, err := core.RawBytesMessageSignBytes(chainID, uniqueID, raw)
	if err != nil {
		return nil, err
	}
	return f.priv.Sign(b)
}

func (f *fakeDeps) ChainID() string             { return "test-chain" }
func (f *fakeDeps) Start(context.Context) error { return nil }
func (f *fakeDeps) Stop(context.Context) error  { return nil }

// TestHealthManager drives every failure reason and its recovery through the checks.
func TestHealthManager(t *testing.T) {
	deps := newFakeDeps()
	store := NewMemoryStore(StoreConfig{})
	t.Cleanup(func() { _ = store.Close() })

	// The probe timeout is generous: the store check does a real pebble write and read, and
	// under -race on a loaded machine that can take far longer than a production probe would.
	m := newHealthManager(healthSettings{probeTimeout: 30 * time.Second, maxBlockAge: time.Minute}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	readiness := func() healthpb.HealthCheckResponse_ServingStatus {
		resp, err := m.hs.Check(ctx, &healthpb.HealthCheckRequest{})
		require.NoError(t, err)
		return resp.GetStatus()
	}

	require.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, readiness())
	m.start(healthDeps{client: deps, signer: deps, pubKey: deps.priv.PubKey(), store: store}, "test-chain")
	m.cycle(ctx)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, readiness())

	healthy := newFakeDeps()
	healthy.priv, healthy.set = deps.priv, deps.set
	restore := func() {
		deps.mu.Lock()
		deps.status, deps.statusErr, deps.set, deps.setErr, deps.reg, deps.moduleErr, deps.signerErr, deps.priv = healthy.status, nil, healthy.set, nil, healthy.reg, nil, nil, healthy.priv
		deps.mu.Unlock()
	}
	failures := []struct {
		check, reason string
		mutate        func()
	}{
		{"app", reasonAppUnreachable, func() { deps.statusErr = errors.New("down") }},
		{"app", reasonChainMismatch, func() { deps.status.ChainID = "other" }},
		{"app", reasonAppSyncing, func() { deps.status.CatchingUp = true }},
		{"app", reasonChainStalled, func() { deps.status.BlockTime = time.Now().Add(-time.Hour) }},
		{"fibre_module", reasonModuleUnavailable, func() { deps.moduleErr = status.Error(codes.Unimplemented, "unknown service") }},
		{"fibre_module", reasonAppUnreachable, func() { deps.moduleErr = status.Error(codes.DeadlineExceeded, "timeout") }},
		{"signer", reasonSignerUnreachable, func() { deps.signerErr = errors.New("down") }},
		{"signer", reasonSignerKeyChanged, func() { deps.priv = ed25519.GenPrivKey() }},
		{"validator", reasonAppUnreachable, func() { deps.setErr = errors.New("down") }},
		{"validator", reasonValidatorInactive, func() { deps.set = validator.Set{ValidatorSet: core.NewValidatorSet(nil), Height: 42} }},
		{"registration", reasonNotRegistered, func() { deps.reg.Found = false }},
		{"registration", reasonHostInvalid, func() { deps.reg.Host = "no-port" }},
	}
	for _, tc := range failures {
		deps.mu.Lock()
		tc.mutate()
		deps.mu.Unlock()
		m.cycle(ctx)
		ready, failing := m.evaluate()
		assert.False(t, ready, tc.reason)
		assert.Equal(t, []string{tc.check}, failing, tc.reason)
		assert.Equal(t, tc.reason, m.results[tc.check].reason, tc.check)
		assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, readiness(), tc.reason)
		restore()
		m.cycle(ctx)
		assert.Equal(t, healthpb.HealthCheckResponse_SERVING, readiness(), "recovery after "+tc.reason)
	}

	// Stop publishes not-ready and ignores later results.
	m.stop()
	m.record("app", passed())
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, readiness())
}

// TestCheckSignerTimeout checks that a stalled signer gives up with the probe deadline
// instead of waiting for the signer client's own timeout, and that the check loop still
// returns on cancellation so shutdown is not held back.
func TestCheckSignerTimeout(t *testing.T) {
	deps := newFakeDeps()
	deps.signerBlock = make(chan struct{})
	t.Cleanup(func() { close(deps.signerBlock) })
	store := NewMemoryStore(StoreConfig{})
	t.Cleanup(func() { _ = store.Close() })

	m := newHealthManager(healthSettings{probeTimeout: 20 * time.Millisecond, maxBlockAge: time.Minute}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.start(healthDeps{client: deps, signer: deps, pubKey: deps.priv.PubKey(), store: store}, "test-chain")

	ctx, cancel := context.WithTimeout(context.Background(), m.settings.probeTimeout)
	defer cancel()
	start := time.Now()
	res := m.checkSigner(ctx)
	assert.Equal(t, reasonSignerUnreachable, res.reason)
	assert.Less(t, time.Since(start), time.Second, "the check does not wait for the signer's own timeout")

	cancelled, stop := context.WithCancel(context.Background())
	stop()
	done := make(chan struct{})
	go func() { defer close(done); m.run(cancelled) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the check loop did not return on cancellation")
	}
}

// TestServerHealth checks the health service on a running server over its TLS listener.
func TestServerHealth(t *testing.T) {
	deps := newFakeDeps()
	cfg := DefaultServerConfig()
	cfg.ServerListenAddress, cfg.UnlimitedBudget = "127.0.0.1:0", true
	cfg.Health.CheckInterval = "50ms"
	cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg.StateClientFn = func() (state.Client, error) { return deps, nil }
	cfg.SignerFn = func(string) (core.PrivValidator, error) { return deps, nil }
	cfg.StoreFn = func(_ context.Context, scfg StoreConfig) (*Store, error) { return NewMemoryStore(scfg), nil }
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, srv.Start(ctx))

	conn, err := grpclib.NewClient(srv.ListenAddress(), grpclib.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // self-signed identity cert, as grpc_health_probe -tls-no-verify
		MinVersion:         tls.VersionTLS13,
	})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	health := healthpb.NewHealthClient(conn)
	check := func(service string) healthpb.HealthCheckResponse_ServingStatus {
		resp, err := health.Check(ctx, &healthpb.HealthCheckRequest{Service: service})
		if err != nil {
			return healthpb.HealthCheckResponse_UNKNOWN
		}
		return resp.GetStatus()
	}

	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, check(HealthServiceLiveness))
	require.Eventually(t, func() bool { return check("") == healthpb.HealthCheckResponse_SERVING }, 10*time.Second, 20*time.Millisecond)

	stream, err := health.Watch(ctx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	_, err = stream.Recv()
	assert.Equal(t, codes.Unimplemented, status.Code(err), "Watch is rejected so it cannot hold the drain")

	require.NoError(t, srv.Stop(ctx))
	assert.Equal(t, healthpb.HealthCheckResponse_UNKNOWN, check(""), "listener closed")
}

// TestStoreProbe checks that a probe gives up when its context ends, that a probe started while
// another is in flight waits for it rather than failing, and that Close waits for a running probe
// or, if it is stuck, closes the database once the probe returns.
func TestStoreProbe(t *testing.T) {
	cfg := DefaultStoreConfig()
	cfg.Path = t.TempDir()
	store, err := NewStore(t.Context(), cfg)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, store.Probe(ctx, []byte("a")))

	expired, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, store.Probe(expired, []byte("b")), context.Canceled)

	store.probe <- struct{}{} // as if a probe were still in pebble
	busy, cancelBusy := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancelBusy()
	require.ErrorIs(t, store.Probe(busy, []byte("c")), context.DeadlineExceeded, "a waiting probe gives up with its context")
	go func() {
		time.Sleep(50 * time.Millisecond)
		<-store.probe
	}()
	require.NoError(t, store.Probe(ctx, []byte("c")), "a probe waits for the one in flight instead of failing")

	store.probe <- struct{}{}
	go func() {
		time.Sleep(50 * time.Millisecond)
		<-store.probe
	}()
	require.NoError(t, store.Close(), "Close waits for the running probe")

	// A stuck probe: Close gives up, then the database is closed once the probe returns,
	// so the store can be opened again in the same process.
	store, err = NewStore(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { probeCloseWait = 5 * time.Second })
	probeCloseWait = 20 * time.Millisecond
	store.probe <- struct{}{}
	require.ErrorContains(t, store.Close(), "still running")
	<-store.probe
	require.Eventually(t, func() bool {
		reopened, err := NewStore(t.Context(), cfg)
		if err != nil {
			return false
		}
		require.NoError(t, reopened.Close())
		return true
	}, 5*time.Second, 20*time.Millisecond)
}
