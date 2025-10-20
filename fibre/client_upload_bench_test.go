package fibre_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/fibre"
	"github.com/celestiaorg/celestia-app/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app/v6/fibre/validator"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"github.com/celestiaorg/go-square/v3/share"
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
//	1 KiB          ~20.7 ms   ~0.047 MiB/s  ~29.3 MB     ~253k
//	16 KiB         ~20.8 ms   ~0.75 MiB/s   ~29.3 MB     ~252.5k
//	128 KiB        ~20.8 ms   ~6.0 MiB/s    ~29.2 MB     ~251k
//	1 MiB          ~23.5 ms   ~42.6 MiB/s   ~41.9 MB     ~250k
//	128 MiB (max)  ~725 ms    ~177 MiB/s    ~1,776 MB    ~294k
//
// Key observations:
//   - Small blobs (<=128 KiB): overhead-dominated, fixed ~20.8ms cost limits throughput
//   - Medium blobs (1 MiB): ~23.5ms with ~42.6 MiB/s throughput
//   - Large blobs (128 MiB): throughput reaches ~177 MiB/s as encoding work dominates
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
			// Create benchmark-optimized client with 100 validators
			ctx := context.Background()
			client := makeBenchmarkClient(&testing.T{}, 100)
			defer func() { _ = client.Close() }()

			namespace := share.MustNewV0Namespace([]byte("bench"))

			// Pre-generate random data to exclude from benchmark
			data := make([]byte, bm.numBytes)
			_, err := rand.Read(data)
			require.NoError(b, err)

			for b.Loop() {
				blob, err := fibre.NewBlob(data, client.Config().BlobConfig)
				require.NoError(b, err)

				result, err := client.Upload(ctx, namespace, blob)
				require.NoError(b, err)
				require.NotEmpty(b, result.ValidatorSignatures)
			}

			// Calculate and report throughput
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
//	128 KiB      1              ~20.4 ms    ~6.1 MiB/s     ~29.2 MB     ~251k
//	128 KiB      4              ~27.9 ms    ~17.9 MiB/s    ~114 MB      ~986k
//	128 KiB      8              ~42.5 ms    ~23.5 MiB/s    ~223 MB      ~1.94M
//	1 MiB        1              ~23.5 ms    ~42.6 MiB/s    ~41.9 MB     ~250k
//	1 MiB        4              ~42.1 ms    ~95.1 MiB/s    ~165.7 MB    ~987k
//	1 MiB        8              ~80.1 ms    ~99.9 MiB/s    ~329 MB      ~1.96M
//	1 MiB        16             ~168 ms     ~95.4 MiB/s    ~647 MB      ~3.85M
//	128 MiB      1              ~716 ms     ~179 MiB/s     ~1,776 MB    ~294k
//	128 MiB      4              ~2,791 ms   ~184 MiB/s     ~7,103 MB    ~1.18M
//
// Key observations:
//   - Small blobs (128 KiB): good concurrency scaling from ~6.1 to ~23.5 MiB/s aggregate at concurrency 8
//   - Medium blobs (1 MiB): peak throughput at concurrency 8 (~99.9 MiB/s)
//   - Medium blobs (1 MiB): slight diminishing returns at concurrency 16 (~95.4 MiB/s, 4.5% drop) due to coordination overhead
//   - Large blobs (128 MiB): best aggregate throughput at concurrency 4 (~184 MiB/s)
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
			defer func() { _ = client.Close() }()

			namespace := share.MustNewV0Namespace([]byte("bench"))

			// Pre-generate random data to exclude from benchmark
			data := make([]byte, bm.blobSize)
			_, err := rand.Read(data)
			require.NoError(b, err)

			for b.Loop() {
				blob, err := fibre.NewBlob(data, client.Config().BlobConfig)
				require.NoError(b, err)

				// Launch concurrent uploads
				errChan := make(chan error, bm.concurrency)
				for range bm.concurrency {
					go func() {
						_, err := client.Upload(ctx, namespace, blob)
						errChan <- err
					}()
				}

				// Wait for all uploads to complete
				for range bm.concurrency {
					err := <-errChan
					require.NoError(b, err)
				}
			}

			// Calculate and report aggregate throughput
			// Each iteration processes bm.concurrency blobs
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

func (b *benchmarkValidatorClient) UploadRows(ctx context.Context, req *types.UploadRowsRequest, opts ...grpclib.CallOption) (*types.UploadRowsResponse, error) {
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

	return &types.UploadRowsResponse{
		ValidatorSignature: b.cachedSignature,
	}, nil
}

func (b *benchmarkValidatorClient) DownloadRows(ctx context.Context, req *types.DownloadRowsRequest, opts ...grpclib.CallOption) (*types.DownloadRowsResponse, error) {
	// Benchmarks don't use downloads, return empty response
	return &types.DownloadRowsResponse{Rows: nil}, nil
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
	client, err := fibre.NewClient(nil, makeTestKeyring(t), &mockValidatorSetGetter{set: valSet}, &mockHostRegistry{}, cfg)
	require.NoError(t, err)
	return client
}
