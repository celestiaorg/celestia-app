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

// fakeDeps is a state.Client and a core.PrivValidator whose answers tests change under mu.
type fakeDeps struct {
	mu        sync.Mutex
	status    state.NodeStatus
	statusErr error
	set       validator.Set
	setErr    error
	reg       state.ProviderRegistration
	moduleErr error
	priv      crypto.PrivKey
	signerErr error
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

func (f *fakeDeps) GetByHeight(ctx context.Context, _ uint64) (validator.Set, error) {
	return f.Head(ctx)
}

func (f *fakeDeps) SignVote(string, *cmtproto.Vote) error         { return nil }
func (f *fakeDeps) SignProposal(string, *cmtproto.Proposal) error { return nil }
func (f *fakeDeps) ChainID() string                               { return "test-chain" }
func (f *fakeDeps) Start(context.Context) error                   { return nil }
func (f *fakeDeps) Stop(context.Context) error                    { return nil }

func (f *fakeDeps) GetHost(context.Context, *core.Validator) (validator.Host, error) {
	return "", errors.New("unused")
}

func (f *fakeDeps) VerifyPromise(context.Context, *state.PaymentPromise) (state.VerifiedPromise, error) {
	return state.VerifiedPromise{}, nil
}

// TestHealthManager drives every failure reason and its recovery through the checks.
func TestHealthManager(t *testing.T) {
	deps := newFakeDeps()
	store := NewMemoryStore(StoreConfig{})
	t.Cleanup(func() { _ = store.Close() })

	m := newHealthManager(healthSettings{probeTimeout: time.Second, maxBlockAge: time.Minute}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	readiness := func() healthpb.HealthCheckResponse_ServingStatus {
		resp, err := m.hs.Check(ctx, &healthpb.HealthCheckRequest{Service: HealthServiceReadiness})
		require.NoError(t, err)
		return resp.GetStatus()
	}

	require.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, readiness())
	m.start(healthDeps{client: deps, signer: deps, pubKey: deps.priv.PubKey(), store: store}, "test-chain")
	m.cycle(ctx)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, readiness())
	require.Equal(t, "ready", m.report().Status)

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
		{checkApp, reasonAppUnreachable, func() { deps.statusErr = errors.New("down") }},
		{checkApp, reasonChainMismatch, func() { deps.status.ChainID = "other" }},
		{checkApp, reasonAppSyncing, func() { deps.status.CatchingUp = true }},
		{checkApp, reasonChainStalled, func() { deps.status.BlockTime = time.Now().Add(-time.Hour) }},
		{checkModule, reasonModuleUnavailable, func() { deps.moduleErr = status.Error(codes.Unimplemented, "unknown service") }},
		{checkModule, reasonAppUnreachable, func() { deps.moduleErr = status.Error(codes.DeadlineExceeded, "timeout") }},
		{checkSigner, reasonSignerUnreachable, func() { deps.signerErr = errors.New("down") }},
		{checkSigner, reasonSignerKeyChanged, func() { deps.priv = ed25519.GenPrivKey() }},
		{checkValidator, reasonAppUnreachable, func() { deps.setErr = errors.New("down") }},
		{checkValidator, reasonValidatorInactive, func() { deps.set = validator.Set{ValidatorSet: core.NewValidatorSet(nil), Height: 42} }},
		{checkRegistration, reasonNotRegistered, func() { deps.reg.Found = false }},
		{checkRegistration, reasonHostInvalid, func() { deps.reg.Host = "no-port" }},
	}
	for _, tc := range failures {
		deps.mu.Lock()
		tc.mutate()
		deps.mu.Unlock()
		m.cycle(ctx)
		rep := m.report()
		assert.Equal(t, tc.reason, rep.Checks[tc.check].Reason, tc.check)
		assert.Equal(t, []string{tc.check}, rep.FailedChecks, tc.reason)
		assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, readiness(), tc.reason)
		restore()
		m.cycle(ctx)
		assert.Equal(t, healthpb.HealthCheckResponse_SERVING, readiness(), "recovery after "+tc.reason)
	}

	// Stop publishes not-ready and ignores later results.
	m.stop()
	m.record(checkApp, ok())
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, readiness())
	assert.Equal(t, phaseStopping, m.report().Phase)
}

// TestServerHealth checks gRPC and HTTP health on a running server with real listeners.
func TestServerHealth(t *testing.T) {
	deps := newFakeDeps()
	cfg := DefaultServerConfig()
	cfg.ServerListenAddress, cfg.HealthListenAddress, cfg.UnlimitedBudget = "127.0.0.1:0", "127.0.0.1:0", true
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
	readyz := func() (int, HealthReport) {
		resp, err := http.Get("http://" + srv.HealthListenAddress() + "/readyz") //nolint:gosec // test URL
		require.NoError(t, err)
		defer resp.Body.Close()
		var rep HealthReport
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&rep))
		return resp.StatusCode, rep
	}

	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, check(HealthServiceLiveness))
	require.Eventually(t, func() bool { return check("") == healthpb.HealthCheckResponse_SERVING }, 10*time.Second, 20*time.Millisecond)
	code, rep := readyz()
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "ready", rep.Status)

	stream, err := health.Watch(ctx, &healthpb.HealthCheckRequest{Service: HealthServiceReadiness})
	require.NoError(t, err)
	_, err = stream.Recv()
	assert.Equal(t, codes.Unimplemented, status.Code(err), "Watch is rejected so it cannot hold the drain")

	require.NoError(t, srv.Stop(ctx))
	assert.Equal(t, healthpb.HealthCheckResponse_UNKNOWN, check(HealthServiceReadiness), "listener closed")
}
