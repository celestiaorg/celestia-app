package fibre_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
)

// hangingSetGetter blocks Head/GetByHeight until the context is cancelled,
// simulating a hung app node. It returns the context's error so callers see
// the reason the query was aborted (deadline vs. cancellation).
type hangingSetGetter struct{}

func (hangingSetGetter) Head(ctx context.Context) (validator.Set, error) {
	<-ctx.Done()
	return validator.Set{}, ctx.Err()
}

func (hangingSetGetter) GetByHeight(ctx context.Context, _ uint64) (validator.Set, error) {
	<-ctx.Done()
	return validator.Set{}, ctx.Err()
}

// newClientWithStateGetter builds and starts a client whose validator-set
// lookups are served by getter, letting tests drive the state-query timeout and
// cancellation paths without any real app node.
func newClientWithStateGetter(t *testing.T, cfg fibre.ClientConfig, getter validator.SetGetter) *fibre.Client {
	t.Helper()
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: getter, chainID: "celestia"}, nil
	}
	client, err := fibre.NewClient(makeTestKeyring(t), cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, client.Stop(context.Background())) })
	return client
}

// A hung app node must abort the Upload's initial validator-set lookup at
// RPCTimeout rather than blocking forever, so a caller with no deadline of its
// own is still protected.
func TestClientUploadStateQueryTimeoutAbortsFast(t *testing.T) {
	cfg := fibre.DefaultClientConfig()
	cfg.RPCTimeout = 50 * time.Millisecond

	client := newClientWithStateGetter(t, cfg, hangingSetGetter{})
	blob := makeTestBlobV0(t, 256*1024)

	start := time.Now()
	_, err := client.Upload(context.Background(), testNamespace, blob)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "getting validator set")
	require.GreaterOrEqual(t, elapsed, cfg.RPCTimeout)
	require.Less(t, elapsed, time.Second)
}

// The same guarantee holds for Download's initial validator-set lookup.
func TestClientDownloadStateQueryTimeoutAbortsFast(t *testing.T) {
	cfg := fibre.DefaultClientConfig()
	cfg.RPCTimeout = 50 * time.Millisecond

	client := newClientWithStateGetter(t, cfg, hangingSetGetter{})
	blob := makeTestBlobV0(t, 256*1024)

	start := time.Now()
	_, err := client.Download(context.Background(), blob.ID())
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "getting validator set")
	require.Less(t, elapsed, time.Second)
}

// A caller cancellation must unwind the operation promptly and win over the
// (larger) RPC timeout — WithTimeout only tightens.
func TestClientUploadCallerCancellationReturnsPromptly(t *testing.T) {
	cfg := fibre.DefaultClientConfig()
	cfg.RPCTimeout = 5 * time.Second

	client := newClientWithStateGetter(t, cfg, hangingSetGetter{})
	blob := makeTestBlobV0(t, 256*1024)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := client.Upload(ctx, testNamespace, blob)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, elapsed, time.Second)
}

// hangingUploadClient blocks in UploadShard until its RPC context is cancelled,
// then records that it observed the cancellation. Shared across all validators
// so the test can assert at least one in-flight peer RPC was cancelled.
type hangingUploadClient struct {
	started   chan struct{}
	startOnce *sync.Once
	sawCancel *atomic.Bool
}

func (h *hangingUploadClient) UploadShard(ctx context.Context, _ *types.UploadShardRequest, _ ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	h.startOnce.Do(func() { close(h.started) })
	<-ctx.Done()
	h.sawCancel.Store(true)
	return nil, ctx.Err()
}

func (h *hangingUploadClient) DownloadShard(context.Context, *types.DownloadShardRequest, ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	return nil, context.Canceled
}

func (h *hangingUploadClient) Close() error { return nil }

// Cancelling the caller mid-upload must propagate into the in-flight per-peer
// RPC context. That propagation is the mechanism by which a cancellation
// reaches the server so it can stop its own commit.
func TestClientUploadCancellationPropagatesToPeerRPC(t *testing.T) {
	validators, _ := makeTestValidators(t, 10)
	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}

	cfg := fibre.DefaultClientConfig()
	// Large RPC timeout so the caller cancel — not a timeout — is what unblocks
	// the hung peer RPC.
	cfg.RPCTimeout = 30 * time.Second

	started := make(chan struct{})
	var startOnce sync.Once
	var sawCancel atomic.Bool
	cfg.NewClientFn = func(context.Context, *core.Validator) (fibregrpc.Client, error) {
		return &hangingUploadClient{started: started, startOnce: &startOnce, sawCancel: &sawCancel}, nil
	}

	client := newClientWithStateGetter(t, cfg, &mockValidatorSetGetter{set: valSet})
	blob := makeTestBlobV0(t, 256*1024)

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := client.Upload(ctx, testNamespace, blob)
		errc <- err
	}()

	select {
	case <-started:
	case err := <-errc:
		t.Fatalf("upload returned before reaching the peer: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("upload RPC never reached the peer")
	}

	cancel()

	select {
	case err := <-errc:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not return after cancellation")
	}
	require.True(t, sawCancel.Load(), "in-flight peer RPC context was not cancelled")
}
