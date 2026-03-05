package fibre_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/celestiaorg/celestia-app/v8/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v8/fibre/state"
	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	clock "github.com/filecoin-project/go-clock"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
)

// BenchmarkClient_Upload measures the performance of the Upload operation with mocked validators.
// This benchmark isolates client-side performance by mocking out network and server overhead.
//
// Run with: go test -bench=BenchmarkClient_Upload$ -benchmem -count=5 -run=^$ -timeout=15m
//
// CPU: AMD Ryzen 9 7940HS w/ Radeon 780M Graphics
// Results with 100 validators (averaged over 5 iterations):
//
//	Blob Size      Time/op    Throughput    Memory/op    Allocs/op
//	1 KiB          ~21.6 ms   ~0.046 MiB/s  ~25.6 MB     ~200.9k
//	16 KiB         ~18.2 ms   ~0.86 MiB/s   ~25.5 MB     ~200.7k
//	128 KiB        ~18.1 ms   ~6.9 MiB/s    ~25.4 MB     ~198.9k
//	1 MiB          ~21.2 ms   ~47.2 MiB/s   ~38.2 MB     ~198.0k
//	128 MiB (max)  ~845 ms    ~154 MiB/s    ~1,771 MB    ~240.0k
//
// Key observations:
//   - Small blobs (<=128 KiB): overhead-dominated, fixed ~18-22ms cost limits throughput
//   - Medium blobs (1 MiB): ~21.2ms with ~47.2 MiB/s throughput
//   - Large blobs (128 MiB): throughput reaches ~154 MiB/s as encoding work dominates
//   - Throughput scales with blob size as fixed overhead becomes less significant
func BenchmarkClient_Upload(b *testing.B) {
	benchmarks := []struct {
		name     string
		sizeKiB  int
		numBytes int
	}{
		{"1_KiB", 1, 1 * 1024},
		{"16_KiB", 16, 16 * 1024},
		{"128_KiB", 128, 128 * 1024},
		{"1_MiB", 1024, 1 * 1024 * 1024},
		{"128_MiB_max", 128 * 1024, 128 * 1024 * 1024},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// create benchmark-optimized client with 100 validators
			ctx := context.Background()
			client := makeBenchmarkClient(&testing.T{}, 100)
			defer func() { _ = client.Stop(ctx) }()

			namespace := share.MustNewV0Namespace([]byte("bench"))

			// pre-generate random data to exclude from benchmark
			data := make([]byte, bm.numBytes)
			_, err := rand.Read(data)
			require.NoError(b, err)

			for b.Loop() {
				blob, err := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
				require.NoError(b, err)

				result, err := client.Upload(ctx, namespace, blob)
				require.NoError(b, err)
				require.NotEmpty(b, result.ValidatorSignatures)
			}

			// calculate and report throughput
			bytesProcessed := int64(b.N) * int64(bm.numBytes)
			b.ReportMetric(float64(bytesProcessed)/b.Elapsed().Seconds()/(1024*1024), "MiB/s")
		})
	}
}

// BenchmarkClient_Upload_Concurrent measures concurrent Upload operations across different blob sizes.
//
// Run with: go test -bench=BenchmarkClient_Upload_Concurrent -benchmem -count=5 -run=^$ -timeout=30m
//
// CPU: AMD Ryzen 9 7940HS w/ Radeon 780M Graphics
// Results with 100 validators (averaged over 5 iterations):
//
//	Blob Size    Concurrency    Time/op     Throughput     Memory/op    Allocs/op
//	128 KiB      1              ~17.8 ms    ~7.0 MiB/s     ~25.4 MB     ~198.9k
//	128 KiB      4              ~26.5 ms    ~18.9 MiB/s    ~101.8 MB    ~796k
//	128 KiB      8              ~54.1 ms    ~18.7 MiB/s    ~203.5 MB    ~1.59M
//	1 MiB        1              ~22.8 ms    ~44.2 MiB/s    ~38.2 MB     ~198.0k
//	1 MiB        4              ~40.9 ms    ~97.9 MiB/s    ~152.7 MB    ~792k
//	1 MiB        8              ~80.3 ms    ~99.8 MiB/s    ~305.4 MB    ~1.58M
//	1 MiB        16             ~167 ms     ~95.5 MiB/s    ~610.8 MB    ~3.17M
//	128 MiB      1              ~852 ms     ~151 MiB/s     ~1,772 MB    ~242k
//	128 MiB      4              ~2,680 ms   ~191 MiB/s     ~7,088 MB    ~976k
//
// Key observations:
//   - Small blobs (128 KiB): good concurrency scaling from ~7.0 to ~18.9 MiB/s aggregate at concurrency 4
//   - Medium blobs (1 MiB): peak throughput at concurrency 8 (~99.8 MiB/s), slight drop at 16 due to overhead
//   - Medium blobs (1 MiB): strong throughput gains from concurrency 4 (~97.9 MiB/s) to 8 (~99.8 MiB/s)
//   - Large blobs (128 MiB): best aggregate throughput at concurrency 4 (~191 MiB/s, 1.26x single upload)
//   - Concurrency benefits increase with blob size as encoding work parallelizes better
func BenchmarkClient_Upload_Concurrent(b *testing.B) {
	benchmarks := []struct {
		name        string
		blobSize    int
		concurrency int
	}{
		{"128_KiB/concurrency_1", 128 * 1024, 1},
		{"128_KiB/concurrency_4", 128 * 1024, 4},
		{"128_KiB/concurrency_8", 128 * 1024, 8},
		{"1_MiB/concurrency_1", 1 * 1024 * 1024, 1},
		{"1_MiB/concurrency_4", 1 * 1024 * 1024, 4},
		{"1_MiB/concurrency_8", 1 * 1024 * 1024, 8},
		{"1_MiB/concurrency_16", 1 * 1024 * 1024, 16},
		{"128_MiB_max/concurrency_1", 128 * 1024 * 1024, 1},
		{"128_MiB_max/concurrency_4", 128 * 1024 * 1024, 4},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			ctx := context.Background()
			client := makeBenchmarkClient(&testing.T{}, 100)
			defer func() { _ = client.Stop(ctx) }()

			namespace := share.MustNewV0Namespace([]byte("bench"))

			// pre-generate random data to exclude from benchmark
			data := make([]byte, bm.blobSize)
			_, err := rand.Read(data)
			require.NoError(b, err)

			for b.Loop() {
				// launch concurrent uploads
				errChan := make(chan error, bm.concurrency)
				for range bm.concurrency {
					go func() {
						blob, err := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
						require.NoError(b, err)

						_, err = client.Upload(ctx, namespace, blob)
						errChan <- err
					}()
				}

				// wait for all uploads to complete
				for range bm.concurrency {
					err := <-errChan
					require.NoError(b, err)
				}
			}

			// calculate and report aggregate throughput
			// each iteration processes bm.concurrency blobs
			bytesProcessed := int64(b.N) * int64(bm.concurrency) * int64(bm.blobSize)
			b.ReportMetric(float64(bytesProcessed)/b.Elapsed().Seconds()/(1024*1024), "MiB/s")
		})
	}
}

// benchmarkValidatorClient provides minimal mock functionality for benchmarks.
// It signs the promise on first call and caches the signature for subsequent calls.
// Uses sync.Once for lock-free caching after initialization.
type benchmarkValidatorClient struct {
	validator       *core.Validator
	privKey         cmted25519.PrivKey
	once            sync.Once
	cachedSignature []byte
}

// makeBenchmarkMockClient creates a fast mock client optimized for benchmarks.
// It shares private keys across all validators but creates per-validator clients.
func makeBenchmarkMockClient(validators []*core.Validator, privKeys []cmted25519.PrivKey) grpc.NewClientFn {
	// Map validator addresses to private keys
	privKeyMap := make(map[string]cmted25519.PrivKey)
	for i, val := range validators {
		privKeyMap[val.Address.String()] = privKeys[i]
	}

	return func(ctx context.Context, val *core.Validator) (grpc.Client, error) {
		return &benchmarkValidatorClient{
			validator: val,
			privKey:   privKeyMap[val.Address.String()],
		}, nil
	}
}

func (b *benchmarkValidatorClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	// Sign the real PaymentPromise data on first call and cache it.
	// We must sign the actual PaymentPromise because SignatureSet verifies signatures against
	// the real sign bytes. Since we use a mocked clock, all PaymentPromises have the same
	// CreationTimestamp, making their SignBytes identical, so we can safely cache the signature.
	// sync.Once ensures lock-free reads after the first initialization.
	b.once.Do(func() {
		var pp fibre.PaymentPromise
		if err := pp.FromProto(req.Promise); err != nil {
			// Can't return error from Do(), will be caught by SignatureSet verification
			return
		}

		signBytes, err := pp.SignBytes()
		if err != nil {
			return
		}

		// Sign with validator's private key
		privKeyBytes := b.privKey.Bytes()
		b.cachedSignature = ed25519.Sign(ed25519.PrivateKey(privKeyBytes), signBytes)
	})

	return &types.UploadShardResponse{
		ValidatorSignature: b.cachedSignature,
	}, nil
}

func (b *benchmarkValidatorClient) DownloadShard(ctx context.Context, req *types.DownloadShardRequest, opts ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	// Benchmarks don't use downloads, return empty response
	return &types.DownloadShardResponse{Shard: nil}, nil
}

func (b *benchmarkValidatorClient) Close() error {
	return nil
}

// makeBenchmarkClient creates a client optimized for benchmarks with minimal server overhead.
// Uses a mocked clock to ensure all PaymentPromises have the same CreationTimestamp.
func makeBenchmarkClient(t *testing.T, numValidators int) *fibre.Client {
	validators, privKeys := makeTestValidators(t, numValidators)
	mockClientFn := makeBenchmarkMockClient(validators, privKeys)

	cfg := fibre.DefaultClientConfig()
	cfg.NewClientFn = mockClientFn
	// Use mocked clock with fixed time to make all PaymentPromises identical
	mockClock := clock.NewMock()
	mockClock.Set(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg.Clock = mockClock

	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(validators), Height: 100}
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: &mockValidatorSetGetter{set: valSet}, chainID: "celestia"}, nil
	}
	client, err := fibre.NewClient(makeTestKeyring(t), cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	return client
}
