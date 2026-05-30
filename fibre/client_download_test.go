package fibre_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v9/fibre/state"
	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
)

func TestClientDownload(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"Success", testClientDownloadSuccess},
		{"Success_Concurrent", testClientDownloadConcurrent},
		{"FaultTolerance", testClientDownloadFaultTolerance},
		{"ContextCancellation", testClientDownloadContextCancellation},
		{"CancelDrainsInflight", testClientDownloadCancelDrainsInflight},
		{"ClosedClient", testClientDownloadClosedClient},
		{"LargeValidatorFailure", testClientDownloadLargeValidatorFailure},
		{"IncorrectRowDistribution", testClientDownloadIncorrectRowDistribution},
		{"WithHeight", testClientDownloadWithHeight},
		{"WithZeroHeight", testClientDownloadWithZeroHeight},
		{"TamperedBlobRLC", testClientDownloadTamperedBlob},
		{"MaliciousSubmitterCollusion", testClientDownloadMaliciousSubmitter},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.fn)
	}
}

func testClientDownloadSuccess(t *testing.T) {
	blob := makeTestBlobV0(t, 256*1024)
	client := makeTestDownloadClient(t, 10, nil, blob)
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	downloaded, err := client.Download(t.Context(), blob.ID())
	defer downloaded.Free()
	require.NoError(t, err)
	require.NotNil(t, downloaded)
	require.Equal(t, blob.Data(), downloaded.Data())
}

func testClientDownloadConcurrent(t *testing.T) {
	const numConcurrent = 5

	blobs := make([]*fibre.Blob, numConcurrent)
	for i := range numConcurrent {
		blobs[i] = makeTestBlobV0(t, 256*1024)
	}

	client := makeTestDownloadClient(t, 100, nil, blobs...)
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	var wg sync.WaitGroup
	for _, blob := range blobs {
		wg.Add(1)
		go func(blob *fibre.Blob) {
			defer wg.Done()

			downloaded, err := client.Download(t.Context(), blob.ID())
			defer downloaded.Free()
			require.NoError(t, err)
			require.Equal(t, blob.Data(), downloaded.Data())
		}(blob)
	}
	wg.Wait()
}

func testClientDownloadContextCancellation(t *testing.T) {
	blob := makeTestBlobV0(t, 256*1024)
	client := makeTestDownloadClient(t, 10, nil, blob)
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	downloaded, err := client.Download(ctx, blob.ID())
	defer downloaded.Free()
	require.ErrorIs(t, err, context.Canceled)
}

// testClientDownloadCancelDrainsInflight verifies that cancelling a download
// mid-flight does not let Download return while a worker is still in flight.
// The worker may still be storing into the pool slab the download is about to
// free, so Blob must drain it first. Pre-fix, Download returned immediately on
// cancel and that store raced the slab release.
func testClientDownloadCancelDrainsInflight(t *testing.T) {
	blob := makeTestBlobV0(t, 256*1024)

	entered := make(chan struct{})
	release := make(chan struct{})
	client := makeTestDownloadClient(t, 1, func(cfg *fibre.ClientConfig) {
		inner := cfg.NewClientFn
		cfg.NewClientFn = func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
			c, err := inner(ctx, val)
			if err != nil {
				return nil, err
			}
			return &blockingDownloadClient{Client: c, entered: entered, release: release}, nil
		}
	}, blob)
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := client.Download(ctx, blob.ID())
		done <- err
	}()

	<-entered // a worker is in flight, parked inside DownloadShard
	cancel()  // cancel while it is still running

	// Download must keep waiting for the in-flight worker to drain.
	select {
	case <-done:
		t.Fatal("Download returned while a worker was still in flight")
	case <-time.After(100 * time.Millisecond):
	}

	close(release) // worker completes; the drain can now finish
	require.ErrorIs(t, <-done, context.Canceled)
}

func testClientDownloadClosedClient(t *testing.T) {
	blob := makeTestBlobV0(t, 256*1024)
	client := makeTestDownloadClient(t, 10, nil, blob)
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	require.NoError(t, client.Stop(t.Context()))
	require.NoError(t, client.Stop(t.Context())) // idempotent

	downloaded, err := client.Download(t.Context(), blob.ID())
	defer downloaded.Free()
	require.ErrorIs(t, err, fibre.ErrClientClosed)
}

func testClientDownloadFaultTolerance(t *testing.T) {
	// test failure tolerance boundaries with 10 validators
	// Each validator gets ~1229 rows out of 4096 originalRows needed.
	// 3 successes yield ~3687 rows (< 4096), 4 successes yield ~4916 rows (>= 4096).
	const numValidators = 10
	blob := makeTestBlobV0(t, 256*1024)

	tests := []struct {
		failures  int
		expectErr error
	}{
		{10, fibre.ErrNotFound},
		{7, fibre.ErrNotEnoughShards}, // 3 successes, ~3687 unique rows < 4096
		{6, nil},                      // 4 successes, ~4916 unique rows >= 4096
		{5, nil},                      // 5 successes, more than enough
		{4, nil},                      // 6 successes, more than enough
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%d_failures", tc.failures), func(t *testing.T) {
			client := makeTestDownloadClient(t, numValidators, func(cfg *fibre.ClientConfig) {
				cfg.NewClientFn = failingClientFn(tc.failures, cfg.NewClientFn)
			}, blob)
			t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

			downloaded, err := client.Download(t.Context(), blob.ID())
			defer downloaded.Free()
			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, blob.Data(), downloaded.Data())
			}
		})
	}
}

func testClientDownloadLargeValidatorFailure(t *testing.T) {
	// Test that when a large validator fails, small validators compensate.
	// Stakes: 1 large (1000) + 9 small (100 each). Total: 1900.
	// Large validator gets min(ceil(4096 * 1000 * 3 / 1900), 4096) = 4096 rows.
	// Small validators get ceil(4096 * 100 * 3 / 1900) = 647 rows each.
	// When the large validator fails, the coordinator dynamically launches
	// small validators until enough unique rows are collected.
	// 7 small validators provide ~4529 unique rows >= 4096 originalRows.
	blob := makeTestBlobV0(t, 256*1024)

	const largeStake int64 = 1000
	stakes := []int64{largeStake, 100, 100, 100, 100, 100, 100, 100, 100, 100}
	client := makeTestDownloadClientWithStakes(t, stakes, func(cfg *fibre.ClientConfig) {
		// Fail the large validator by stake, not by call order.
		// Select uses non-deterministic shuffle, so the large validator
		// may not be contacted first.
		inner := cfg.NewClientFn
		cfg.NewClientFn = func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
			if val.VotingPower >= largeStake {
				return failingClient{}, nil
			}
			return inner(ctx, val)
		}
	}, blob)
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	downloaded, err := client.Download(t.Context(), blob.ID())
	defer downloaded.Free()
	require.NoError(t, err)
	require.Equal(t, blob.Data(), downloaded.Data())
}

func testClientDownloadIncorrectRowDistribution(t *testing.T) {
	// Test that download succeeds when the server-side row distribution doesn't
	// match the client's view. Validators have distinct stakes (100, 200, ..., 1000).
	// The server distributes rows using the original stakes, while the client sees
	// the same validators with reshuffled stakes, producing a different per-validator
	// row assignment. The coordinator should adapt dynamically.
	const numValidators = 10
	blob := makeTestBlobV0(t, 256*1024)

	// Create validators with distinct increasing stakes: 100, 200, ..., 1000
	stakes := make([]int64, numValidators)
	for i := range numValidators {
		stakes[i] = int64((i + 1) * 100)
	}
	validators, privKeys := makeTestValidatorsWithStakes(t, stakes)

	// Server-side validator set uses the original stakes
	serverValSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}

	// Client-side: same validators but with reshuffled stakes.
	// Reverse the stake assignment so each validator has a different stake
	// than what the server used (e.g. validator with stake 100 now has 1000).
	reshuffledValidators := make([]*core.Validator, numValidators)
	for i, v := range validators {
		reshuffledValidators[i] = &core.Validator{
			Address:     v.Address,
			PubKey:      v.PubKey,
			VotingPower: stakes[numValidators-1-i],
		}
	}
	clientValSet := validator.Set{ValidatorSet: core.NewValidatorSet(reshuffledValidators), Height: 100}

	// Verify precondition: the two sets produce different per-validator row assignments.
	// NewValidatorSet sorts by voting power, so positional row counts are the same,
	// but different addresses hold different stakes, so Assign maps rows differently.
	blobCfg := blob.Config()
	cfg := fibre.DefaultClientConfig()
	serverAssign := serverValSet.Assign(blob.ID().Commitment(), blobCfg.TotalRows(), blobCfg.OriginalRows, cfg.MinRowsPerValidator, cfg.LivenessThreshold)
	clientAssign := clientValSet.Assign(blob.ID().Commitment(), blobCfg.TotalRows(), blobCfg.OriginalRows, cfg.MinRowsPerValidator, cfg.LivenessThreshold)
	// Build per-address row count maps and verify they differ
	serverByAddr := make(map[string]int)
	for v, rows := range serverAssign {
		serverByAddr[v.Address.String()] = len(rows)
	}
	clientByAddr := make(map[string]int)
	for v, rows := range clientAssign {
		clientByAddr[v.Address.String()] = len(rows)
	}
	require.NotEqual(t, serverByAddr, clientByAddr, "test requires different per-validator row assignments")

	// Mock servers use the server-side validator set for row assignment
	cfg.NewClientFn = makeDownloadMockClientFn(serverValSet, &cfg, privKeys, blob)
	// Client uses the reshuffled-stakes validator set for row estimation
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: &mockValidatorSetGetter{set: clientValSet}}, nil
	}

	client, err := fibre.NewClient(makeTestKeyring(t), cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	// Despite the mismatched row distribution, download should succeed
	// and return the correct data.
	downloaded, err := client.Download(t.Context(), blob.ID())
	defer downloaded.Free()
	require.NoError(t, err)
	require.Equal(t, blob.Data(), downloaded.Data())
}

func testClientDownloadWithHeight(t *testing.T) {
	// Test that passing a specific height to Download uses GetByHeight
	// instead of Head, and the download succeeds with the correct data.
	blob := makeTestBlobV0(t, 256*1024)
	validators, privKeys := makeTestValidators(t, 10)

	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 42}
	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = makeDownloadMockClientFn(valSet, &cfg, privKeys, blob)

	getter := &heightTrackingSetGetter{set: valSet}
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: getter}, nil
	}

	client, err := fibre.NewClient(makeTestKeyring(t), cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	downloaded, err := client.Download(t.Context(), blob.ID(), fibre.WithHeight(42))
	defer downloaded.Free()
	require.NoError(t, err)
	require.Equal(t, blob.Data(), downloaded.Data())

	// Verify GetByHeight was called with the correct height, not Head.
	require.Equal(t, int64(1), getter.getByHeightCalls.Load(), "expected GetByHeight to be called once")
	require.Equal(t, int64(0), getter.headCalls.Load(), "expected Head to not be called")
	require.Equal(t, uint64(42), getter.lastHeight.Load(), "expected GetByHeight to be called with height 42")
}

func testClientDownloadWithZeroHeight(t *testing.T) {
	// Test that zero height (the default) falls through to Head()
	// instead of calling GetByHeight(0), which would be rejected by the gRPC layer.
	blob := makeTestBlobV0(t, 256*1024)
	validators, privKeys := makeTestValidators(t, 10)

	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 42}
	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = makeDownloadMockClientFn(valSet, &cfg, privKeys, blob)

	getter := &heightTrackingSetGetter{set: valSet}
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: getter}, nil
	}

	client, err := fibre.NewClient(makeTestKeyring(t), cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	downloaded, err := client.Download(t.Context(), blob.ID())
	defer downloaded.Free()
	require.NoError(t, err)
	require.Equal(t, blob.Data(), downloaded.Data())

	// Verify Head was called, not GetByHeight.
	require.Equal(t, int64(1), getter.headCalls.Load(), "expected Head to be called once")
	require.Equal(t, int64(0), getter.getByHeightCalls.Load(), "expected GetByHeight to not be called")
}

// heightTrackingSetGetter tracks which methods are called and with what arguments.
type heightTrackingSetGetter struct {
	set              validator.Set
	headCalls        atomic.Int64
	getByHeightCalls atomic.Int64
	lastHeight       atomic.Uint64
}

func (g *heightTrackingSetGetter) Head(ctx context.Context) (validator.Set, error) {
	g.headCalls.Add(1)
	return g.set, nil
}

func (g *heightTrackingSetGetter) GetByHeight(ctx context.Context, height uint64) (validator.Set, error) {
	g.getByHeightCalls.Add(1)
	g.lastHeight.Store(height)
	return g.set, nil
}

// makeTestDownloadClient creates a download client with equal-stake validators that serves the given blobs.
func makeTestDownloadClient(
	t *testing.T,
	numValidators int,
	customCfg func(*fibre.ClientConfig),
	blobs ...*fibre.Blob,
) *fibre.Client {
	t.Helper()
	validators, privKeys := makeTestValidators(t, numValidators)
	return makeTestDownloadClientFromValidators(t, validators, privKeys, customCfg, blobs...)
}

// makeTestDownloadClientWithStakes creates a download client with custom-stake validators.
func makeTestDownloadClientWithStakes(
	t *testing.T,
	stakes []int64,
	customCfg func(*fibre.ClientConfig),
	blobs ...*fibre.Blob,
) *fibre.Client {
	t.Helper()
	validators, privKeys := makeTestValidatorsWithStakes(t, stakes)
	return makeTestDownloadClientFromValidators(t, validators, privKeys, customCfg, blobs...)
}

// makeTestDownloadClientFromValidators creates a download client from the given validators.
func makeTestDownloadClientFromValidators(
	t *testing.T,
	validators []*core.Validator,
	privKeys []cmted25519.PrivKey,
	customCfg func(*fibre.ClientConfig),
	blobs ...*fibre.Blob,
) *fibre.Client {
	t.Helper()

	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}
	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = makeDownloadMockClientFn(valSet, &cfg, privKeys, blobs...)
	if customCfg != nil {
		customCfg(&cfg)
	}

	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: &mockValidatorSetGetter{set: valSet}}, nil
	}
	client, err := fibre.NewClient(makeTestKeyring(t), cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	return client
}

// makeDownloadMockClientFn creates a mock client function that uses valSet.Assign()
// for realistic row distribution matching the production code.
func makeDownloadMockClientFn(
	valSet validator.Set,
	cfg *fibre.ClientConfig,
	privKeys []cmted25519.PrivKey,
	blobs ...*fibre.Blob,
) grpc.NewClientFn {
	// Build address -> privKey map for lookup regardless of validator ordering
	privKeyByAddr := make(map[string]cmted25519.PrivKey)
	for _, pk := range privKeys {
		privKeyByAddr[pk.PubKey().Address().String()] = pk
	}

	return func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
		pk, ok := privKeyByAddr[val.Address.String()]
		if !ok {
			return nil, fmt.Errorf("validator not found: %s", val.Address)
		}

		client := &downloadMockClient{
			validator: val,
			privKey:   pk,
			blobs:     blobs,
			valSet:    valSet,
			clientCfg: cfg,
		}
		return client, nil
	}
}

// mock infrastructure

// blockingDownloadClient gates DownloadShard: it signals once a worker has
// entered (entered) and blocks until the test unblocks it (release), letting the
// test pin a worker in flight while it cancels the download.
type blockingDownloadClient struct {
	grpc.Client
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingDownloadClient) DownloadShard(ctx context.Context, req *types.DownloadShardRequest, opts ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return b.Client.DownloadShard(ctx, req, opts...)
}

type downloadMockClient struct {
	validator *core.Validator
	privKey   cmted25519.PrivKey
	blobs     []*fibre.Blob
	valSet    validator.Set
	clientCfg *fibre.ClientConfig
}

func (d *downloadMockClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	return &types.UploadShardResponse{}, nil
}

func (d *downloadMockClient) DownloadShard(ctx context.Context, req *types.DownloadShardRequest, opts ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	var id fibre.BlobID
	if err := id.UnmarshalBinary(req.BlobId); err != nil {
		return nil, err
	}

	// find the blob matching the commitment
	var blob *fibre.Blob
	for _, b := range d.blobs {
		if b.ID().Commitment() == id.Commitment() {
			blob = b
			break
		}
	}
	if blob == nil {
		return &types.DownloadShardResponse{}, nil
	}

	// Use Assign to determine which rows this validator should return
	blobCfg := blob.Config()
	shardMap := d.valSet.Assign(
		id.Commitment(),
		blobCfg.TotalRows(),
		blobCfg.OriginalRows,
		d.clientCfg.MinRowsPerValidator,
		d.clientCfg.LivenessThreshold,
	)

	// Find our validator in the set (for ShardMap key matching)
	val, ok := d.valSet.GetByAddress(d.validator.Address)
	if !ok {
		return &types.DownloadShardResponse{}, nil
	}

	rowIndices := shardMap[val]
	rows := make([]*types.BlobRow, 0, len(rowIndices))
	for _, idx := range rowIndices {
		row, err := blob.Row(idx)
		if err != nil {
			continue
		}
		rows = append(rows, &types.BlobRow{
			Index: uint32(row.Index),
			Data:  row.Row,
			Proof: row.RowProof,
		})
	}

	shard := &types.BlobShard{
		Rows: rows,
	}

	shard.Coefficients = rlc.Marshal(blob.RLC())

	return &types.DownloadShardResponse{Shard: shard}, nil
}

func (d *downloadMockClient) Close() error {
	return nil
}

// tamperedMockClient models a network of two coexisting valid encodings —
// an honest blob and a separate "bad" blob with its own commitment. Validators
// in the malicious set serve bad-blob shards; the rest serve honest-blob
// shards. The mock answers requests for either commitment, but bad-commitment
// requests are only honored by malicious validators (others return empty as
// if they had no data for that commitment).
type tamperedMockClient struct {
	validator  *core.Validator
	honestBlob *fibre.Blob
	badBlob    *fibre.Blob
	malicious  map[string]bool
	blobCfg    fibre.BlobConfig
	valSet     validator.Set
	clientCfg  *fibre.ClientConfig
}

func (d *tamperedMockClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	return &types.UploadShardResponse{}, nil
}

func (d *tamperedMockClient) DownloadShard(ctx context.Context, req *types.DownloadShardRequest, opts ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	var id fibre.BlobID
	if err := id.UnmarshalBinary(req.BlobId); err != nil {
		return nil, err
	}

	requestedHonest := id.Commitment() == d.honestBlob.ID().Commitment()
	requestedBad := id.Commitment() == d.badBlob.ID().Commitment()
	if !requestedHonest && !requestedBad {
		return &types.DownloadShardResponse{}, nil
	}

	malicious := d.malicious[d.validator.Address.String()]
	// Bad-commitment requests are only served by colluding (malicious)
	// validators; honest validators have no data for that commitment.
	if requestedBad && !malicious {
		return &types.DownloadShardResponse{}, nil
	}

	source := d.honestBlob
	if malicious {
		source = d.badBlob
	}

	shardMap := d.valSet.Assign(
		id.Commitment(),
		d.blobCfg.TotalRows(),
		d.blobCfg.OriginalRows,
		d.clientCfg.MinRowsPerValidator,
		d.clientCfg.LivenessThreshold,
	)

	val, ok := d.valSet.GetByAddress(d.validator.Address)
	if !ok {
		return &types.DownloadShardResponse{}, nil
	}

	rowIndices := shardMap[val]
	rows := make([]*types.BlobRow, 0, len(rowIndices))
	for _, idx := range rowIndices {
		proof, err := source.Row(idx)
		if err != nil {
			continue
		}
		rows = append(rows, &types.BlobRow{
			Index: uint32(proof.Index),
			Data:  proof.Row,
			Proof: proof.RowProof,
		})
	}

	shard := &types.BlobShard{
		Rows: rows,
	}

	shard.Coefficients = rlc.Marshal(source.RLC())

	return &types.DownloadShardResponse{Shard: shard}, nil
}

func (d *tamperedMockClient) Close() error {
	return nil
}

// makeTamperedDownloadMockClientFn creates a mock client factory where
// validators in the malicious set serve rows from badBlob and the rest serve
// rows from honestBlob. Both blobs are independently valid; they just commit
// to different bytes.
func makeTamperedDownloadMockClientFn(
	valSet validator.Set,
	cfg *fibre.ClientConfig,
	privKeys []cmted25519.PrivKey,
	honestBlob *fibre.Blob,
	badBlob *fibre.Blob,
	malicious map[string]bool,
	blobCfg fibre.BlobConfig,
) grpc.NewClientFn {
	privKeyByAddr := make(map[string]cmted25519.PrivKey)
	for _, pk := range privKeys {
		privKeyByAddr[pk.PubKey().Address().String()] = pk
	}

	return func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
		if _, ok := privKeyByAddr[val.Address.String()]; !ok {
			return nil, fmt.Errorf("validator not found: %s", val.Address)
		}
		return &tamperedMockClient{
			validator:  val,
			honestBlob: honestBlob,
			badBlob:    badBlob,
			malicious:  malicious,
			blobCfg:    blobCfg,
			valSet:     valSet,
			clientCfg:  cfg,
		}, nil
	}
}

func testClientDownloadTamperedBlob(t *testing.T) {
	// Test recovery in the realistic threat model: an honest uploader pushed
	// the blob, but some validators serve a locally-tampered version with
	// their own merkle paths. Malicious shards fail batched commitment
	// verification at the client (their row root, combined with the original
	// RLC root, doesn't hash to the on-chain commitment) and get rejected
	// wholesale; honest shards verify and contribute their rows toward
	// reconstruction.
	const numValidators = 10

	honest := makeTestBlobV0(t, 256*1024)
	decoy := makeTestBlobV0(t, 256*1024) // an independent valid encoding with a different commitment

	validators, privKeys := makeTestValidators(t, numValidators)
	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}

	// Mark every other validator as malicious. With 10 validators at 1/3
	// liveness threshold, each validator covers ~3K/10 of the K+N=3K rows
	// non-overlappingly, so 5 honest validators have ~1.5K rows — well
	// above the K threshold needed to reconstruct.
	malicious := make(map[string]bool, numValidators/2)
	for i := 0; i < numValidators; i += 2 {
		malicious[validators[i].Address.String()] = true
	}

	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = makeTamperedDownloadMockClientFn(valSet, &cfg, privKeys, honest, decoy, malicious, honest.Config())
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: &mockValidatorSetGetter{set: valSet}}, nil
	}

	client, err := fibre.NewClient(makeTestKeyring(t), cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	downloaded, err := client.Download(t.Context(), honest.ID())
	defer downloaded.Free()
	require.NoError(t, err)
	require.NotNil(t, downloaded)

	// The recovered data must match the original blob's data and commitment.
	require.Equal(t, honest.Data(), downloaded.Data())
	require.Equal(t, honest.ID().Commitment(), downloaded.ID().Commitment())
}

func testClientDownloadMaliciousSubmitter(t *testing.T) {
	// Threat model: the submitter built the row tree over tampered data and
	// computed the RLC from those same tampered rows, so the bad commitment
	// is fully self-consistent. They distributed those bad shards to every
	// validator. The protocol intentionally cannot tell the data is "bad"
	// — the commitment binds, not the data's correctness. What it does
	// guarantee is determinism: every downloader of the same commitment
	// reconstructs the same bytes, regardless of which validators served
	// them. So the application can rely on commitment-equals-content while
	// remaining responsible for deciding whether that commitment is the one
	// it expected.
	const numValidators = 10

	honest := makeTestBlobV0(t, 256*1024)
	bad := makeTestBlobV0(t, 256*1024) // a separate, fully-consistent encoding with its own commitment

	validators, privKeys := makeTestValidators(t, numValidators)
	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}

	// Every validator was handed the bad shards by the submitter.
	holdsBad := make(map[string]bool, numValidators)
	for _, v := range validators {
		holdsBad[v.Address.String()] = true
	}

	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = makeTamperedDownloadMockClientFn(valSet, &cfg, privKeys, honest, bad, holdsBad, honest.Config())
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: &mockValidatorSetGetter{set: valSet}}, nil
	}

	client, err := fibre.NewClient(makeTestKeyring(t), cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, client.Stop(t.Context())) })

	first, err := client.Download(t.Context(), bad.ID())
	defer first.Free()
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, bad.ID().Commitment(), first.ID().Commitment())
	require.NotEqual(t, honest.Data(), first.Data(), "reconstructed data is the malicious version, not the honest one")

	// Repeat the download: even though shard selection and worker order are
	// non-deterministic, the reconstructed bytes must match exactly.
	second, err := client.Download(t.Context(), bad.ID())
	defer second.Free()
	require.NoError(t, err)
	require.Equal(t, first.Data(), second.Data(), "downloads of the same commitment must be byte-identical")
}
