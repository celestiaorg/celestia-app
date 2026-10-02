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

func TestDefaultClientConfigDetachesBackgroundUploads(t *testing.T) {
	require.True(t, fibre.DefaultClientConfig().DetachBackgroundUploads)
}

// uploadGate holds the UploadShard RPCs of the gated validators until it opens
// or their RPC context ends, then reports each RPC's outcome.
type uploadGate struct {
	held     int
	started  atomic.Int64
	entered  chan struct{} // closed once all held RPCs have started
	release  chan struct{} // closed to let the held RPCs proceed
	results  chan error    // one entry per held RPC once it ends
	openOnce sync.Once
}

func (g *uploadGate) open() { g.openOnce.Do(func() { close(g.release) }) }

type gatedUploadClient struct {
	fibregrpc.Client
	gate *uploadGate
}

func (c *gatedUploadClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpc.CallOption) (*types.UploadShardResponse, error) {
	if int(c.gate.started.Add(1)) == c.gate.held {
		close(c.gate.entered)
	}
	select {
	case <-c.gate.release:
	case <-ctx.Done():
	}
	if err := ctx.Err(); err != nil {
		c.gate.results <- err
		return nil, err
	}
	resp, err := c.Client.UploadShard(ctx, req, opts...)
	c.gate.results <- err
	return resp, err
}

// newGatedUploadClient builds a started client over 4 equal-stake mock
// validators and gates the first `held` validators the client dials. With one
// held, Upload reaches quorum (3 of 4) without it; with two, it cannot.
func newGatedUploadClient(t *testing.T, detach bool, held int) (*fibre.Client, *uploadGate) {
	t.Helper()
	validators, privKeys := makeTestValidators(t, 4)
	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}
	gate := &uploadGate{
		held:    held,
		entered: make(chan struct{}),
		release: make(chan struct{}),
		results: make(chan error, held),
	}
	t.Cleanup(gate.open)

	next := makeMockClientFn(validators, privKeys)
	var dialed atomic.Int64
	cfg := fibre.DefaultClientConfig()
	cfg.DetachBackgroundUploads = detach
	cfg.NewClientFn = func(ctx context.Context, val *core.Validator) (fibregrpc.Client, error) {
		c, err := next(ctx, val)
		if err != nil || dialed.Add(1) > int64(held) {
			return c, err
		}
		return &gatedUploadClient{Client: c, gate: gate}, nil
	}
	return newClientWithStateGetter(t, cfg, &mockValidatorSetGetter{set: valSet}), gate
}

func awaitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func awaitResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("held upload never ended")
		return nil
	}
}

// Without the flag, cancelling right after Upload returns drops the held upload.
func TestUploadCancelAfterQuorumDropsBackgroundUploads(t *testing.T) {
	client, gate := newGatedUploadClient(t, false, 1)
	ctx, cancel := context.WithCancel(t.Context())
	_, err := client.Upload(ctx, testNamespace, makeTestBlobV0(t, 256*1024))
	require.NoError(t, err)
	awaitSignal(t, gate.entered, "held uploads to start")

	cancel()
	require.ErrorIs(t, awaitResult(t, gate.results), context.Canceled)
}

// With the flag, the held upload outlives the cancel and completes.
func TestUploadCancelAfterQuorumKeepsBackgroundUploads(t *testing.T) {
	client, gate := newGatedUploadClient(t, true, 1)
	ctx, cancel := context.WithCancel(t.Context())
	_, err := client.Upload(ctx, testNamespace, makeTestBlobV0(t, 256*1024))
	require.NoError(t, err)
	awaitSignal(t, gate.entered, "held uploads to start")

	cancel()
	select {
	case err := <-gate.results:
		t.Fatalf("held upload ended on the caller's cancel: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	gate.open()
	require.NoError(t, awaitResult(t, gate.results))
	client.Await()
}

// With the flag, cancelling before quorum still aborts the upload and its
// in-flight RPCs.
func TestUploadCancelBeforeQuorumAbortsDetachedUploads(t *testing.T) {
	client, gate := newGatedUploadClient(t, true, 2)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := client.Upload(ctx, testNamespace, makeTestBlobV0(t, 256*1024))
		done <- err
	}()
	awaitSignal(t, gate.entered, "held uploads to start")

	cancel()
	require.ErrorIs(t, awaitResult(t, done), context.Canceled)
	for range 2 {
		require.ErrorIs(t, awaitResult(t, gate.results), context.Canceled)
	}
}
