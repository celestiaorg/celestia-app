package fibre_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// waitForAllUploadsTimeout mirrors the unexported bound in client_upload.go.
const waitForAllUploadsTimeout = 5 * time.Second

func TestDefaultClientConfigWaitsForAllUploads(t *testing.T) {
	require.True(t, fibre.DefaultClientConfig().WaitForAllUploads)
}

// gatedValidator controls the one validator whose shard upload is held back,
// so Upload reaches quorum (3 of 4 equal stakes) without it.
type gatedValidator struct {
	entered   chan struct{} // closed when the held RPC starts
	release   chan struct{} // closed to let the held RPC proceed
	delivered chan struct{} // closed when the held shard reached the validator
	resumed   chan error    // the RPC context's error at the moment it resumed
	ignoreCtx bool          // hold until the gate opens even if the RPC context ends
	openOnce  sync.Once
}

func (g *gatedValidator) open() { g.openOnce.Do(func() { close(g.release) }) }

// gatedUploadClient holds UploadShard until the gate opens or the RPC context
// ends, then reports that context's error and, if still live, delegates to the
// real mock validator.
type gatedUploadClient struct {
	fibregrpc.Client
	gate      *gatedValidator
	enterOnce sync.Once
}

func (c *gatedUploadClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpc.CallOption) (*types.UploadShardResponse, error) {
	c.enterOnce.Do(func() { close(c.gate.entered) })
	if c.gate.ignoreCtx {
		<-c.gate.release
	} else {
		select {
		case <-c.gate.release:
		case <-ctx.Done():
		}
	}
	err := ctx.Err()
	select {
	case c.gate.resumed <- err:
	default:
	}
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.UploadShard(ctx, req, opts...)
	if err == nil {
		close(c.gate.delivered)
	}
	return resp, err
}

// newGatedUploadClient builds a started client over 4 equal-stake mock
// validators; the first validator the client dials is gated.
func newGatedUploadClient(t *testing.T, waitAll bool) (*fibre.Client, *gatedValidator) {
	t.Helper()
	validators, privKeys := makeTestValidators(t, 4)
	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}
	gate := &gatedValidator{
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
		delivered: make(chan struct{}),
		resumed:   make(chan error, 1),
	}
	t.Cleanup(gate.open)

	next := makeMockClientFn(validators, privKeys)
	var dialed atomic.Int64
	cfg := fibre.DefaultClientConfig()
	cfg.WaitForAllUploads = waitAll
	cfg.NewClientFn = func(ctx context.Context, val *core.Validator) (fibregrpc.Client, error) {
		c, err := next(ctx, val)
		if err != nil || dialed.Add(1) != 1 {
			return c, err
		}
		return &gatedUploadClient{Client: c, gate: gate}, nil
	}
	return newClientWithStateGetter(t, cfg, &mockValidatorSetGetter{set: valSet}), gate
}

// startUpload runs Upload in the background and reports its error.
func startUpload(t *testing.T, ctx context.Context, client *fibre.Client) <-chan error {
	t.Helper()
	blob := makeTestBlobV0(t, 256*1024)
	done := make(chan error, 1)
	go func() {
		_, err := client.Upload(ctx, testNamespace, blob)
		done <- err
	}()
	return done
}

func awaitWaitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func awaitUploadReturn(t *testing.T, done <-chan error) error {
	t.Helper()
	return awaitUploadReturnWithin(t, done, waitForAllUploadsTimeout+5*time.Second)
}

// awaitUploadReturnWithin fails unless Upload returns within d.
func awaitUploadReturnWithin(t *testing.T, done <-chan error, d time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		t.Fatalf("Upload did not return within %s", d)
		return nil
	}
}

// promptly is well under waitForAllUploadsTimeout, so an Upload returning
// within it was not released by the wait's timer.
const promptly = time.Second

func requireUploadPending(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("Upload returned while a remaining upload was in flight: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

// With the flag off Upload returns at quorum without the held validator.
func TestUploadWithoutWaitReturnsAtQuorum(t *testing.T) {
	client, gate := newGatedUploadClient(t, false)
	done := startUpload(t, t.Context(), client)

	awaitWaitSignal(t, gate.entered, "held upload to start")
	require.NoError(t, awaitUploadReturn(t, done))
	select {
	case <-gate.delivered:
		t.Fatal("held shard delivered before the gate opened")
	default:
	}
}

// With the flag on Upload returns only once the remaining upload finished, so
// a cancel right after Upload no longer drops it.
func TestUploadWaitsForRemainingUploads(t *testing.T) {
	client, gate := newGatedUploadClient(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := startUpload(t, ctx, client)

	awaitWaitSignal(t, gate.entered, "held upload to start")
	requireUploadPending(t, done)

	gate.open()
	require.NoError(t, awaitUploadReturnWithin(t, done, promptly))
	select {
	case <-gate.delivered:
	default:
		t.Fatal("Upload returned before the held shard was delivered")
	}
}

// The wait is bounded: past the timeout Upload returns with the quorum, and
// the remaining upload behaves as without the flag.
func TestUploadWaitIsBounded(t *testing.T) {
	client, gate := newGatedUploadClient(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	start := time.Now()
	done := startUpload(t, ctx, client)

	awaitWaitSignal(t, gate.entered, "held upload to start")
	require.NoError(t, awaitUploadReturn(t, done))
	require.GreaterOrEqual(t, time.Since(start), waitForAllUploadsTimeout)

	cancel()
	select {
	case err := <-gate.resumed:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("held upload never resumed")
	}
}

// A caller cancel during the wait ends it at once; quorum was already reached,
// so Upload still succeeds. The held upload ignores the cancel, so only the
// wait's own ctx check can end it.
func TestUploadWaitEndsOnCallerCancel(t *testing.T) {
	client, gate := newGatedUploadClient(t, true)
	gate.ignoreCtx = true
	defer gate.open() // release the held upload before the client's graceful Stop
	ctx, cancel := context.WithCancel(t.Context())
	done := startUpload(t, ctx, client)

	awaitWaitSignal(t, gate.entered, "held upload to start")
	requireUploadPending(t, done)

	cancel()
	require.NoError(t, awaitUploadReturnWithin(t, done, promptly))
}

// Stopping the client ends the wait at once.
func TestUploadWaitEndsOnStop(t *testing.T) {
	client, gate := newGatedUploadClient(t, true)
	done := startUpload(t, t.Context(), client)

	awaitWaitSignal(t, gate.entered, "held upload to start")
	requireUploadPending(t, done)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // forced stop: do not drain the held upload
	_ = client.Stop(ctx)
	require.NoError(t, awaitUploadReturnWithin(t, done, promptly))
}
