package fibre_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/celestiaorg/celestia-app/v9/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v9/fibre/state"
	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	clock "github.com/filecoin-project/go-clock"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
)

// BenchmarkClient_Upload measures the performance of the Upload operation with
// mocked validators, isolating client-side cost (network and server overhead
// are mocked out). It sweeps blob size at concurrency 1 (the plain single-upload
// case) and a few sizes across higher concurrency levels, where each iteration
// fans out N independent uploads.
//
// Run with: go test -bench=BenchmarkClient_Upload$ -benchmem -count=5 -run=^$ -timeout=40m
//
// CPU: AMD Ryzen AI 9 HX 370 w/ Radeon 890M
// Results with 100 validators:
//
//	Blob Size      Concurrency    Time/op     Throughput     Memory/op    Allocs/op
//	256 KiB        1              ~4.0 ms     ~61 MiB/s      ~11.0 MB     ~6.6k
//	1 MiB          1              ~5.2 ms     ~191 MiB/s     ~11.1 MB     ~6.6k
//	16 MiB         1              ~59 ms      ~273 MiB/s     ~17.9 MB     ~6.8k
//	32 MiB         1              ~130 ms     ~245 MiB/s     ~18.0 MB     ~6.8k
//	128 MiB (max)  1              ~510 ms     ~250 MiB/s     ~18.2 MB     ~6.9k
//	256 KiB        4              ~7.8 ms     ~127 MiB/s     ~43.7 MB     ~26.2k
//	256 KiB        8              ~13.8 ms    ~145 MiB/s     ~84.7 MB     ~52.4k
//	1 MiB          4              ~12.1 ms    ~330 MiB/s     ~42.4 MB     ~26.3k
//	1 MiB          8              ~26.4 ms    ~303 MiB/s     ~84.5 MB     ~52.5k
//	1 MiB          16             ~58 ms      ~275 MiB/s     ~169.8 MB    ~104.9k
//	16 MiB         4              ~265 ms     ~241 MiB/s     ~71.8 MB     ~26.9k
//	16 MiB         8              ~485 ms     ~264 MiB/s     ~144 MB      ~53.5k
//	128 MiB (max)  4              ~1.8 s+     ~290 MiB/s     ~72 MB       ~27.0k
//
// Key observations:
//   - Single uploads: throughput plateaus at ~250 MiB/s for >=16 MiB as encoding dominates.
//   - 1 MiB: peaks ~330 MiB/s at concurrency 4, easing off as scheduling overhead grows.
//   - 128 MiB at concurrency 4 is memory-bound (4x multi-hundred-MB live heaps ->
//     GC pressure), so wall time is high-variance; ~290 MiB/s is a best case.
func BenchmarkClient_Upload(b *testing.B) {
	benchmarks := []struct {
		name        string
		numBytes    int
		concurrency int
	}{
		{"256_KiB", 256 * 1024, 1},
		{"1_MiB", 1 * 1024 * 1024, 1},
		{"16_MiB", 16 * 1024 * 1024, 1},
		{"32_MiB", 32 * 1024 * 1024, 1},
		{"128_MiB_max", 128 * 1024 * 1024, 1},
		{"256_KiB/concurrency_4", 256 * 1024, 4},
		{"256_KiB/concurrency_8", 256 * 1024, 8},
		{"1_MiB/concurrency_4", 1 * 1024 * 1024, 4},
		{"1_MiB/concurrency_8", 1 * 1024 * 1024, 8},
		{"1_MiB/concurrency_16", 1 * 1024 * 1024, 16},
		{"16_MiB/concurrency_4", 16 * 1024 * 1024, 4},
		{"16_MiB/concurrency_8", 16 * 1024 * 1024, 8},
		{"128_MiB_max/concurrency_4", 128 * 1024 * 1024, 4},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// create benchmark-optimized client with 100 validators
			ctx := context.Background()
			client := makeBenchmarkClient(&testing.T{}, 100)
			defer func() { _ = client.Stop(ctx) }()

			namespace := share.MustNewV0Namespace([]byte("bench"))

			// pre-generate random data to exclude from benchmark. numBytes names
			// the target blob size, so the payload is that minus the header NewBlob
			// prepends — keeps the labeled size exact and the max within MaxDataSize.
			cfg := fibre.DefaultBlobConfigV0()
			headerLen := fibre.DefaultProtocolParams.MaxBlobSize - cfg.MaxDataSize
			dataSize := bm.numBytes - headerLen
			data := make([]byte, dataSize)
			_, err := rand.Read(data)
			require.NoError(b, err)

			upload := func() error {
				blob, err := fibre.NewBlob(data, cfg)
				if err != nil {
					return err
				}
				defer blob.Free()
				result, err := client.Upload(ctx, namespace, blob)
				if err != nil {
					return err
				}
				if len(result.ValidatorSignatures) == 0 {
					return errors.New("upload returned no validator signatures")
				}
				return nil
			}

			for b.Loop() {
				// concurrency 1 is the plain single-upload case; skip the
				// goroutine/channel machinery for it.
				if bm.concurrency == 1 {
					require.NoError(b, upload())
					continue
				}

				// launch concurrency independent uploads and wait for all.
				errChan := make(chan error, bm.concurrency)
				for range bm.concurrency {
					go func() { errChan <- upload() }()
				}
				for range bm.concurrency {
					require.NoError(b, <-errChan)
				}
			}

			// report aggregate throughput: each iteration uploads concurrency blobs.
			bytesProcessed := int64(b.N) * int64(bm.concurrency) * int64(dataSize)
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
